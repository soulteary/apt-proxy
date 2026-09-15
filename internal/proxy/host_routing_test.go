// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func debianRewriters(t *testing.T) *URLRewriters {
	t.Helper()
	st := newTestState()
	st.SetMirror(distro.TypeDebian, "http://mirrors.example.com/debian/")
	return CreateNewRewriters(distro.TypeDebian, st, newTestRegistry())
}

// Every shape apt can produce for the Debian archives must land on the right
// mirror. The last case is issue #97: the classic sources.list form
//
//	deb http://security.debian.org <suite>-security main
//
// requests /dists/<suite>-security/... with no /debian-security/ prefix, so
// path matching alone never fired and apt got a 404.
func TestDebianRequestShapesRouteCorrectly(t *testing.T) {
	rewriters := debianRewriters(t)

	tests := []struct {
		name string
		host string
		path string
		want string
	}{
		{
			name: "modern sources.list carries the /debian-security/ prefix",
			host: "security.debian.org",
			path: "/debian-security/dists/trixie-security/InRelease",
			want: "http://mirrors.example.com/debian-security/dists/trixie-security/InRelease",
		},
		{
			name: "apt-cacher-ng style host prefix",
			host: "proxy.local",
			path: "/security.debian.org/debian-security/dists/trixie-security/InRelease",
			want: "http://mirrors.example.com/debian-security/dists/trixie-security/InRelease",
		},
		{
			name: "regular debian archive is untouched by host routing",
			host: "deb.debian.org",
			path: "/debian/dists/trixie/InRelease",
			want: "http://mirrors.example.com/debian/dists/trixie/InRelease",
		},
		{
			name: "issue #97: classic sources.list, archive at the host root",
			host: "security.debian.org",
			path: "/dists/trixie-security/InRelease",
			want: "http://mirrors.example.com/debian-security/dists/trixie-security/InRelease",
		},
		{
			name: "issue #97 with an explicit port in Host",
			host: "security.debian.org:80",
			path: "/dists/trixie-security/Release",
			want: "http://mirrors.example.com/debian-security/dists/trixie-security/Release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Host: tt.host, URL: &url.URL{Path: tt.path}}
			RewriteRequestByMode(r, rewriters, distro.TypeDebian)
			if got := r.URL.String(); got != tt.want {
				t.Errorf("rewrote to %q, want %q", got, tt.want)
			}
		})
	}
}

// Host routing must not hijack unrelated hosts.
func TestHostRoutingIgnoresUnrelatedHosts(t *testing.T) {
	rewriters := debianRewriters(t)

	for _, host := range []string{
		"example.com",
		"notsecurity.debian.org",
		"security.debian.org.evil.test",
		"",
	} {
		r := &http.Request{Host: host, URL: &url.URL{Path: "/dists/trixie-security/InRelease"}}
		RewriteRequestByMode(r, rewriters, distro.TypeDebian)
		if got := r.URL.String(); got != "/dists/trixie-security/InRelease" {
			t.Errorf("host %q: rewrote to %q, want the request left alone", host, got)
		}
	}
}

// End-to-end through the real handler, reproducing issue #97: apt asks the
// proxy for the security suite and must get the file, not a 404.
func TestDebianSecurityRootPathEndToEnd(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("InRelease"))
	}))
	defer upstream.Close()

	st := newTestState()
	st.SetMirror(distro.TypeDebian, upstream.URL+"/debian/")
	st.SetProxyMode(distro.TypeDebian)

	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: newTestRegistry(),
		CacheDir: t.TempDir(),
		Logger:   logger.Default(),
		Mode:     distro.TypeDebian,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dists/trixie-security/InRelease", nil)
	req.Host = "security.debian.org"
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (this is the 404 from issue #97)", rec.Code)
	}
	if want := "/debian-security/dists/trixie-security/InRelease"; gotPath != want {
		t.Errorf("upstream saw %q, want %q", gotPath, want)
	}
}
