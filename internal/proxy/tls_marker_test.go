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
	"strings"
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

// With no passthrough allowlist, a request carrying apt-cacher-ng's HTTPS///
// marker must be refused, and above all not quietly proxied somewhere else.
// Such a path normally still contains a distribution segment, so without this
// handling it matched that distribution's pattern and was rewritten onto a
// Ubuntu/Debian mirror -- a request for get.docker.com silently went to a
// Linux distro mirror instead.
//
// The refusal is 403 rather than 501: the marker is implemented now, the
// origin simply is not allowed yet, and the message says which setting to add
// it to.
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
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("via Fiber: status = %d, want 403", resp.StatusCode)
			}

			// And directly, where the authority stays in URL.Host.
			direct := httptest.NewRequest(http.MethodGet, tt.url, nil)
			direct.Host = tt.host
			rec := httptest.NewRecorder()
			ps.ServeHTTP(rec, direct)
			if rec.Code != http.StatusForbidden {
				t.Errorf("direct: status = %d, want 403", rec.Code)
			}
			if body := rec.Body.String(); !strings.Contains(body, "passthrough") {
				t.Errorf("refusal %q should name the setting that would allow it", body)
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

func TestParseTLSRewriteMarkerDetection(t *testing.T) {
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
		if _, _, got := parseTLSRewriteMarker(req); got != tt.want {
			t.Errorf("parseTLSRewriteMarker(%q) found = %v, want %v", tt.url, got, tt.want)
		}
	}
	// The shape Fiber's adaptor produces: authority in Host, URL.Host empty.
	serverForm := httptest.NewRequest(http.MethodGet, "///get.docker.com/ubuntu/dists/stable/InRelease", nil)
	serverForm.URL.Host = ""
	serverForm.Host = "https"
	if _, _, found := parseTLSRewriteMarker(serverForm); !found {
		t.Error("must detect the marker when the authority is only in Host")
	}
	withPort := httptest.NewRequest(http.MethodGet, "///get.docker.com/ubuntu/x", nil)
	withPort.URL.Host = ""
	withPort.Host = "HTTPS:3142"
	if _, _, found := parseTLSRewriteMarker(withPort); !found {
		t.Error("must tolerate a port on the marker authority")
	}
	normal := httptest.NewRequest(http.MethodGet, "/ubuntu/dists/noble/InRelease", nil)
	normal.URL.Host = ""
	normal.Host = "archive.ubuntu.com"
	if _, _, found := parseTLSRewriteMarker(normal); found {
		t.Error("a normal server-form request must not report a marker")
	}

	if _, _, found := parseTLSRewriteMarker(nil); found {
		t.Error("nil request must not report a marker")
	}
	if _, _, found := parseTLSRewriteMarker(&http.Request{}); found {
		t.Error("request with nil URL must not report a marker")
	}
}

// The behaviour issue 57 asked for: with the origin allowlisted, the marker
// resolves to it over TLS and the request is served and cached.
func TestTLSRewriteMarkerReachesAllowlistedOrigin(t *testing.T) {
	ps, seen := passthroughProxy(t, "get.docker.com")

	for _, tt := range []struct{ name, host, url string }{
		{
			name: "proxy form: Host is the HTTPS marker",
			host: "HTTPS",
			url:  "http://HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease",
		},
		{
			name: "rewrite form: marker sits in the path",
			host: "apt-proxy:3142",
			url:  "http://apt-proxy:3142/HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease",
		},
		{
			name: "lowercase marker",
			host: "https",
			url:  "http://https///get.docker.com/linux/ubuntu/dists/jammy/InRelease",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			*seen = recordedRequest{}

			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			ps.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if seen.scheme != "https" {
				t.Errorf("upstream scheme = %q, want https -- the marker means TLS", seen.scheme)
			}
			if seen.host != "get.docker.com" {
				t.Errorf("upstream host = %q, want get.docker.com", seen.host)
			}
			if want := "/linux/ubuntu/dists/jammy/InRelease"; seen.path != want {
				t.Errorf("upstream path = %q, want %q", seen.path, want)
			}
		})
	}
}

// A marker path that contains a distribution segment must still reach its
// origin, not the distribution's mirror. This is the misrouting the old guard
// existed to prevent, now checked on the success path.
func TestTLSRewriteMarkerBeatsDistributionMatching(t *testing.T) {
	ps, seen := passthroughProxy(t, "deb.example.test")

	req := httptest.NewRequest(http.MethodGet,
		"http://HTTPS///deb.example.test/debian/dists/trixie/InRelease", nil)
	req.Host = "HTTPS"
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.host != "deb.example.test" {
		t.Errorf("upstream host = %q; the Debian mirror must not win here", seen.host)
	}
	if want := "/debian/dists/trixie/InRelease"; seen.path != want {
		t.Errorf("upstream path = %q, want %q", seen.path, want)
	}
}

// A marker with nothing after it names no origin: that is a client mistake,
// and 400 says so rather than blaming the allowlist.
func TestTLSRewriteMarkerWithoutOriginIsBadRequest(t *testing.T) {
	ps, seen := passthroughProxy(t, "get.docker.com")

	for _, target := range []string{
		"http://HTTPS///",
		"http://apt-proxy:3142/HTTPS///",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if strings.HasPrefix(target, "http://HTTPS") {
			req.Host = "HTTPS"
		}
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
	if seen.count != 0 {
		t.Errorf("upstream was contacted %d time(s) for a malformed marker", seen.count)
	}
}

// Read methods only, same as ordinary passthrough.
func TestTLSRewriteMarkerRejectsWriteMethods(t *testing.T) {
	ps, seen := passthroughProxy(t, "get.docker.com")

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "http://HTTPS///get.docker.com/linux/ubuntu/x", nil)
		req.Host = "HTTPS"
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
	if seen.count != 0 {
		t.Errorf("upstream was contacted %d time(s) by a non-read method", seen.count)
	}
}

// The origin parsed out of the marker, for both spellings and for the shape
// Fiber's adaptor produces.
func TestParseTLSRewriteMarkerOriginAndPath(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		host       string
		wantOrigin string
		wantPath   string
	}{
		{
			name:       "proxy form",
			url:        "http://HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease",
			host:       "HTTPS",
			wantOrigin: "get.docker.com",
			wantPath:   "/linux/ubuntu/dists/jammy/InRelease",
		},
		{
			name:       "rewrite form",
			url:        "http://apt-proxy:3142/HTTPS///get.docker.com/linux/ubuntu/x",
			host:       "apt-proxy:3142",
			wantOrigin: "get.docker.com",
			wantPath:   "/linux/ubuntu/x",
		},
		{
			name:       "origin only, no path",
			url:        "http://HTTPS///get.docker.com",
			host:       "HTTPS",
			wantOrigin: "get.docker.com",
			wantPath:   "/",
		},
		{
			name:       "marker authority carries a port",
			url:        "http://HTTPS:3142///get.docker.com/x",
			host:       "HTTPS:3142",
			wantOrigin: "get.docker.com",
			wantPath:   "/x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			req.Host = tt.host

			origin, path, found := parseTLSRewriteMarker(req)
			if !found {
				t.Fatal("marker not detected")
			}
			if origin != tt.wantOrigin {
				t.Errorf("origin = %q, want %q", origin, tt.wantOrigin)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

// Same rule as ordinary passthrough: a port the entry pinned is a service
// deliberately listening there, not a default inherited from the request.
func TestTLSRewriteMarkerKeepsAnExplicitlyPinnedPort(t *testing.T) {
	ps, seen := passthroughProxy(t, "archive.example.test:80")

	req := httptest.NewRequest(http.MethodGet,
		"http://HTTPS///archive.example.test:80/repo/dists/x/InRelease", nil)
	req.Host = "HTTPS"
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.host != "archive.example.test:80" {
		t.Errorf("upstream host = %q, want the pinned port kept", seen.host)
	}
	if seen.scheme != "https" {
		t.Errorf("upstream scheme = %q, want https", seen.scheme)
	}
}

// An IPv6 origin named by the marker must stay bracketed, for the same reason
// it must on the ordinary passthrough path.
func TestTLSRewriteMarkerKeepsIPv6Bracketed(t *testing.T) {
	ps, seen := passthroughProxy(t, "[2001:db8::1]")

	for _, origin := range []string{"[2001:db8::1]", "[2001:db8::1]:443"} {
		*seen = recordedRequest{}

		req := httptest.NewRequest(http.MethodGet,
			"http://HTTPS///"+origin+"/repo/dists/x/InRelease", nil)
		req.Host = "HTTPS"
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", origin, rec.Code)
		}
		if seen.host != "[2001:db8::1]" {
			t.Errorf("%s: upstream host = %q, want the address bracketed", origin, seen.host)
		}
	}
}
