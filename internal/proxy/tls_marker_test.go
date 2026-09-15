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
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// fiberApp mounts ps exactly as the daemon does (app.All("/*",
// adaptor.HTTPHandler(...))). That conversion moves the authority from
// URL.Host into Host and leaves URL.Host empty, so testing through it is the
// only way to cover what production actually sees -- httptest.NewRequest
// keeps the absolute-form authority in URL.Host and hides the difference.
func fiberApp(ps *PackageStruct) *fiber.App {
	app := fiber.New()
	app.All("/*", adaptor.HTTPHandler(ps))
	return app
}

func markerTestProxy(t *testing.T, upstream string) *PackageStruct {
	t.Helper()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream+"/ubuntu/")
	st.SetMirror(distro.TypeDebian, upstream+"/debian/")
	st.SetProxyMode(distro.TypeAllDistros)

	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: newTestRegistry(),
		CacheDir: t.TempDir(),
		Logger:   logger.Default(),
		Mode:     distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	return ps
}

// A request carrying apt-cacher-ng's HTTPS/// marker must be refused, not
// quietly proxied somewhere else. Such a path normally still contains a
// distribution segment, so before this guard it matched that distribution's
// pattern and was rewritten onto a Ubuntu/Debian mirror -- a request for
// get.docker.com silently went to a Linux distro mirror instead.
func TestTLSRewriteMarkerIsRejectedNotMisrouted(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits++
		_, _ = w.Write([]byte("should never be reached"))
	}))
	defer upstream.Close()

	ps := markerTestProxy(t, upstream.URL)

	tests := []struct {
		name string
		host string
		url  string
	}{
		{
			name: "proxy form: Host is the HTTPS marker",
			host: "HTTPS",
			url:  "http://HTTPS///get.docker.com/ubuntu/dists/stable/InRelease",
		},
		{
			name: "rewrite form: marker sits in the path",
			host: "apt-proxy:3142",
			url:  "http://apt-proxy:3142/HTTPS///get.docker.com/ubuntu/dists/stable/InRelease",
		},
		{
			name: "marker with a debian segment",
			host: "HTTPS",
			url:  "http://HTTPS///deb.example.com/debian/dists/trixie/InRelease",
		},
		{
			name: "lowercase marker",
			host: "https",
			url:  "http://https///get.docker.com/ubuntu/dists/stable/InRelease",
		},
	}

	app := fiberApp(ps)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := upstreamHits

			// Through the Fiber adaptor, as production does.
			req, err := http.NewRequest(http.MethodGet, tt.url, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("via Fiber: status = %d, want 501", resp.StatusCode)
			}

			// And directly, where the authority stays in URL.Host.
			direct := httptest.NewRequest(http.MethodGet, tt.url, nil)
			direct.Host = tt.host
			rec := httptest.NewRecorder()
			ps.ServeHTTP(rec, direct)
			if rec.Code != http.StatusNotImplemented {
				t.Errorf("direct: status = %d, want 501", rec.Code)
			}

			if upstreamHits != before {
				t.Error("request was proxied upstream; it must be refused outright")
			}
		})
	}
}

// Ordinary requests must keep working: the guard must not reject anything that
// merely looks similar.
func TestTLSRewriteMarkerGuardLeavesNormalRequestsAlone(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	ps := markerTestProxy(t, upstream.URL)

	req, err := http.NewRequest(http.MethodGet, "http://archive.ubuntu.com/ubuntu/dists/noble/InRelease", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := fiberApp(ps).Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if want := "/ubuntu/dists/noble/InRelease"; gotPath != want {
		t.Errorf("upstream saw %q, want %q", gotPath, want)
	}
}

func TestHasTLSRewriteMarker(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"http://HTTPS///get.docker.com/ubuntu/dists/stable/InRelease", true},
		{"http://proxy:3142/HTTPS///get.docker.com/ubuntu/x", true},
		{"http://proxy:3142/https///get.docker.com/ubuntu/x", true},
		{"http://archive.ubuntu.com/ubuntu/dists/noble/InRelease", false},
		{"http://deb.debian.org/debian/dists/trixie/InRelease", false},
		// A package legitimately named after the scheme must not trip the guard.
		{"http://archive.ubuntu.com/ubuntu/pool/main/h/https-everywhere/https-everywhere_1.0_all.deb", false},
		{"http://archive.ubuntu.com/ubuntu/pool/main/libh/libhttps/libhttps_1.0_all.deb", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.url, nil)
		if got := hasTLSRewriteMarker(req); got != tt.want {
			t.Errorf("hasTLSRewriteMarker(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
	// The shape Fiber's adaptor produces: authority in Host, URL.Host empty.
	serverForm := httptest.NewRequest(http.MethodGet, "///get.docker.com/ubuntu/dists/stable/InRelease", nil)
	serverForm.URL.Host = ""
	serverForm.Host = "https"
	if !hasTLSRewriteMarker(serverForm) {
		t.Error("must detect the marker when the authority is only in Host")
	}
	withPort := httptest.NewRequest(http.MethodGet, "///get.docker.com/ubuntu/x", nil)
	withPort.URL.Host = ""
	withPort.Host = "HTTPS:3142"
	if !hasTLSRewriteMarker(withPort) {
		t.Error("must tolerate a port on the marker authority")
	}
	normal := httptest.NewRequest(http.MethodGet, "/ubuntu/dists/noble/InRelease", nil)
	normal.URL.Host = ""
	normal.Host = "archive.ubuntu.com"
	if hasTLSRewriteMarker(normal) {
		t.Error("a normal server-form request must not report a marker")
	}

	if hasTLSRewriteMarker(nil) {
		t.Error("nil request must not report a marker")
	}
	if hasTLSRewriteMarker(&http.Request{}) {
		t.Error("request with nil URL must not report a marker")
	}
}
