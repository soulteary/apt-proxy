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

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// A third-party archive whose path merely contains a distribution segment
// must not be answered out of that distribution's mirror. Before the host
// prefix was validated, the first case below returned the *main* Ubuntu
// archive's InRelease for the deadsnakes PPA, with status 200 and nothing in
// the log to say the content came from somewhere else.
func TestThirdPartyArchivesAreNotRewritten(t *testing.T) {
	rewriters := CreateNewRewriters(distro.TypeAllDistros, newTestState(), newTestRegistry())

	tests := []struct {
		name string
		mode int
		path string
	}{
		{
			name: "launchpad PPA",
			mode: distro.TypeUbuntu,
			path: "/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease",
		},
		{
			name: "vendor repo nested under the host",
			mode: distro.TypeUbuntu,
			path: "/download.docker.com/linux/ubuntu/dists/jammy/InRelease",
		},
		{
			name: "prefix segment that is not a host",
			mode: distro.TypeDebian,
			path: "/some/where/debian/dists/bookworm/InRelease",
		},
		{
			name: "vendor repo with a debian segment",
			mode: distro.TypeDebian,
			path: "/apt.example.test/linux/debian/dists/bookworm/InRelease",
		},
		{
			name: "third-party alpine tree",
			mode: distro.TypeAlpine,
			path: "/dl.example.test/pkgs/alpine/v3.19/main/x86_64/APKINDEX.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Host: "apt-proxy.example", URL: &url.URL{Path: tt.path}}
			RewriteRequestByMode(r, rewriters, tt.mode)
			if got := r.URL.String(); got != tt.path {
				t.Errorf("rewrote to %q, want the request left alone (%q)", got, tt.path)
			}
		})
	}
}

// The forms apt-proxy documents must keep working: the native path, and the
// apt-cacher-ng compatibility form with a single mirror host in front.
func TestSupportedPathFormsStillRewrite(t *testing.T) {
	rewriters := CreateNewRewriters(distro.TypeAllDistros, newTestState(), newTestRegistry())

	tests := []struct {
		name string
		mode int
		path string
		want string
	}{
		{
			name: "native ubuntu path",
			mode: distro.TypeUbuntu,
			path: "/ubuntu/dists/jammy/InRelease",
			want: "http://mirrors.example.com/ubuntu/dists/jammy/InRelease",
		},
		{
			name: "apt-cacher-ng host prefix (README example)",
			mode: distro.TypeDebian,
			path: "/ftp.uni-kl.de/debian/dists/bookworm/InRelease",
			want: "http://mirrors.example.com/debian/dists/bookworm/InRelease",
		},
		{
			name: "host prefix carrying a port",
			mode: distro.TypeDebian,
			path: "/deb.debian.org:80/debian/dists/bookworm/InRelease",
			want: "http://mirrors.example.com/debian/dists/bookworm/InRelease",
		},
		{
			name: "IP literal host prefix",
			mode: distro.TypeCentOS,
			path: "/192.168.0.17/centos/7/os/x86_64/repodata/repomd.xml",
			want: "http://mirrors.example.com/centos/7/os/x86_64/repodata/repomd.xml",
		},
		{
			name: "security host prefix keeps the dedicated mirror",
			mode: distro.TypeDebian,
			path: "/security.debian.org/debian-security/dists/trixie-security/InRelease",
			want: "http://mirrors.example.com/debian-security/dists/trixie-security/InRelease",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Host: "apt-proxy.example", URL: &url.URL{Path: tt.path}}
			RewriteRequestByMode(r, rewriters, tt.mode)
			if got := r.URL.String(); got != tt.want {
				t.Errorf("rewrote to %q, want %q", got, tt.want)
			}
		})
	}
}

// End-to-end: a PPA request must come back as a 404 the client can act on,
// and the distribution's mirror must never be contacted for it.
func TestThirdPartyArchiveIs404EndToEnd(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits++
		_, _ = w.Write([]byte("InRelease"))
	}))
	defer upstream.Close()

	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	st.SetProxyMode(distro.TypeUbuntu)

	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: newTestRegistry(),
		CacheDir: t.TempDir(),
		Logger:   logger.Default(),
		Mode:     distro.TypeUbuntu,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease", nil)
	req.Host = "apt-proxy.example"
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (the PPA is not a distribution apt-proxy mirrors)", rec.Code)
	}
	if upstreamHits != 0 {
		t.Errorf("upstream was contacted %d time(s); a PPA request must not reach the Ubuntu mirror", upstreamHits)
	}
}

func TestAcceptableHostPrefix(t *testing.T) {
	tests := []struct {
		prefix string
		want   bool
	}{
		{"", true},
		{"/", true},
		{"/ftp.uni-kl.de", true},
		{"/deb.debian.org", true},
		{"/deb.debian.org:3142", true},
		{"/192.168.0.17", true},
		{"/192.168.0.17:3142", true},
		{"/localhost", true},
		{"/localhost:3142", true},
		{"/ppa.launchpad.net/deadsnakes/ppa", false},
		{"/download.docker.com/linux", false},
		{"/some/where", false},
		{"/deadsnakes", false},
		{"/linux", false},
		{"/.", false},
		{"/host.", false},
	}

	for _, tt := range tests {
		if got := acceptableHostPrefix(tt.prefix); got != tt.want {
			t.Errorf("acceptableHostPrefix(%q) = %v, want %v", tt.prefix, got, tt.want)
		}
	}
}
