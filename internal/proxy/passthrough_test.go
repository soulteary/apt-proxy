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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/passthrough"
)

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// recordedRequest is what the upstream transport saw.
type recordedRequest struct {
	count  int
	method string
	scheme string
	host   string
	path   string
	query  string
}

// passthroughProxy builds a PackageStruct with the given allowlist and an
// upstream transport that records instead of dialling.
//
// Tests cannot point the allowlist at an httptest server: Parse refuses
// loopback and private literals on purpose, which is the behaviour
// TestParseRejectsUnsafeOrMalformed pins. So origins here are public-looking
// names and the transport stands in for the network.
func passthroughProxy(t *testing.T, entries ...string) (*PackageStruct, *recordedRequest) {
	t.Helper()
	list, err := passthrough.Parse(entries)
	if err != nil {
		t.Fatalf("passthrough.Parse(%v): %v", entries, err)
	}
	st := newTestState()
	st.SetProxyMode(distro.TypeAllDistros)

	seen := &recordedRequest{}
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		seen.count++
		seen.method, seen.path, seen.query = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		seen.scheme, seen.host = r.URL.Scheme, r.Host
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("InRelease")),
			Request:    r,
		}, nil
	})

	ps, err := NewPackageStruct(Options{
		State:             st,
		Registry:          newTestRegistry(),
		CacheDir:          t.TempDir(),
		Logger:            logger.Default(),
		Mode:              distro.TypeAllDistros,
		Passthrough:       list,
		TransportOverride: transport,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	return ps, seen
}

// proxyModeRequest is what apt sends when apt-proxy is its HTTP proxy: the
// origin is named by the request authority, not by a path prefix.
func proxyModeRequest(method, origin, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Host = origin
	r.URL.Host = ""
	return r
}

// The headline case: a PPA, which apt-proxy does not mirror, reaches its
// origin because the operator allowlisted it.
func TestPassthroughReachesAllowlistedOrigin(t *testing.T) {
	const origin = "ppa.launchpad.net"
	ps, seen := passthroughProxy(t, origin)

	const path = "/deadsnakes/ppa/ubuntu/dists/jammy/InRelease"
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, proxyModeRequest(http.MethodGet, origin, path))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.count != 1 {
		t.Fatalf("upstream saw %d requests, want 1", seen.count)
	}
	if seen.path != path {
		t.Errorf("upstream saw path %q, want %q", seen.path, path)
	}
	if seen.host != origin {
		t.Errorf("upstream saw Host %q, want %q", seen.host, origin)
	}
	if seen.scheme != "http" {
		t.Errorf("upstream scheme = %q, want http for a bare-host entry", seen.scheme)
	}
}

// Everything not named stays a 404. The allowlist is the whole boundary.
func TestPassthroughDeniesEverythingElse(t *testing.T) {
	ps, _ := passthroughProxy(t, "allowed.example.test")

	for _, origin := range []string{
		"denied.example.test",
		"allowed.example.test.evil.test",
		"evil.test",
	} {
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, proxyModeRequest(http.MethodGet, origin, "/whatever/dists/x/InRelease"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("origin %q: status = %d, want 404", origin, rec.Code)
		}
	}
}

// With no allowlist configured at all, apt-proxy must behave exactly as before.
func TestPassthroughDisabledByDefault(t *testing.T) {
	ps, _ := passthroughProxy(t) // no entries

	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, proxyModeRequest(http.MethodGet, "ppa.launchpad.net", "/deadsnakes/ppa/ubuntu/dists/jammy/InRelease"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when no allowlist is configured", rec.Code)
	}
}

// apt fetches; it does not write. A method that is not a read has no business
// leaving through the allowlist.
func TestPassthroughOnlyReadMethods(t *testing.T) {
	const origin = "ppa.launchpad.net"
	ps, seen := passthroughProxy(t, origin)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, proxyModeRequest(method, origin, "/anything"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", method, rec.Code)
		}
	}
	if seen.count != 0 {
		t.Errorf("upstream was contacted %d time(s) by a non-read method", seen.count)
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, proxyModeRequest(method, origin, "/anything"))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", method, rec.Code)
		}
	}
}

// A distribution apt-proxy mirrors keeps its own routing even if its host is
// also allowlisted: passthrough runs last, after both distribution checks.
func TestDistributionRoutingWinsOverPassthrough(t *testing.T) {
	ps, _ := passthroughProxy(t, "security.debian.org")

	r := proxyModeRequest(http.MethodGet, "security.debian.org", "/dists/trixie-security/InRelease")
	rule := ps.handleExternalURLs(r)
	if rule == nil {
		t.Fatal("expected the Debian security host route to match")
	}
	if rule.OS != distro.TypeDebian {
		t.Errorf("rule.OS = %d, want the Debian rule (passthrough must not pre-empt it)", rule.OS)
	}
	if !strings.Contains(r.URL.String(), "mirrors.example.com") {
		t.Errorf("request went to %q, want the configured Debian mirror", r.URL.String())
	}
}

// https:// entries must upgrade the upstream request. apt speaks http to its
// proxy, so without this an https-only archive answers with a redirect the
// client cannot follow back through apt-proxy.
func TestPassthroughForcesHTTPSWhenConfigured(t *testing.T) {
	ps, _ := passthroughProxy(t, "https://secure.example.test")

	r := proxyModeRequest(http.MethodGet, "secure.example.test", "/repo/dists/x/InRelease")
	if rule := ps.handleExternalURLs(r); rule == nil {
		t.Fatal("allowlisted origin did not match")
	}
	if r.URL.Scheme != "https" {
		t.Errorf("scheme = %q, want https", r.URL.Scheme)
	}
	if r.URL.Host != "secure.example.test" {
		t.Errorf("host = %q, want secure.example.test", r.URL.Host)
	}
}

// A default port carried over from the http request line must not survive the
// scheme upgrade, or the upstream dial lands on the wrong service.
func TestPassthroughDropsDefaultPortWhenForcingHTTPS(t *testing.T) {
	ps, _ := passthroughProxy(t, "https://secure.example.test")

	r := proxyModeRequest(http.MethodGet, "secure.example.test:80", "/repo/InRelease")
	if rule := ps.handleExternalURLs(r); rule == nil {
		t.Fatal("allowlisted origin did not match")
	}
	if r.URL.Host != "secure.example.test" {
		t.Errorf("host = %q, want the port dropped for https", r.URL.Host)
	}
}

// The path is the client's, untouched: passthrough proxies, it does not rewrite.
func TestPassthroughPreservesPathAndQuery(t *testing.T) {
	ps, _ := passthroughProxy(t, "archive.example.test")

	r := proxyModeRequest(http.MethodGet, "archive.example.test", "/a/b%20c/d?x=1&y=2")
	if rule := ps.handleExternalURLs(r); rule == nil {
		t.Fatal("allowlisted origin did not match")
	}
	if got, want := r.URL.EscapedPath(), "/a/b%20c/d"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := r.URL.RawQuery, "x=1&y=2"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
}

// The rule carries no Cache-Control: apt-proxy knows a distribution's TTLs,
// but nothing about a third-party archive, so the origin's headers decide.
func TestPassthroughLeavesCacheControlToTheOrigin(t *testing.T) {
	ps, _ := passthroughProxy(t, "archive.example.test")

	r := proxyModeRequest(http.MethodGet, "archive.example.test", "/pool/x.deb")
	rule := ps.handleExternalURLs(r)
	if rule == nil {
		t.Fatal("allowlisted origin did not match")
	}
	if rule.CacheControl != "" {
		t.Errorf("CacheControl = %q, want empty so the origin decides", rule.CacheControl)
	}
	if rule.Rewrite {
		t.Error("passthrough must not rewrite the URL onto a mirror")
	}
}

// URL-prefix clients address apt-proxy itself, so no origin is named and
// nothing can pass through.
func TestPassthroughIgnoresURLPrefixForm(t *testing.T) {
	ps, _ := passthroughProxy(t, "archive.example.test")

	r := &http.Request{
		Method: http.MethodGet,
		Host:   "apt-proxy.example:3142",
		URL:    &url.URL{Path: "/archive.example.test/pool/x.deb"},
	}
	if rule := ps.handleExternalURLs(r); rule != nil {
		t.Error("a path-embedded origin must not activate passthrough")
	}
}

// An entry that pins a port means a service deliberately listening there.
// Dropping it as if it were a default inherited from the request line would
// send the request to 443 and never reach that service.
func TestPassthroughKeepsAnExplicitlyPinnedTLSPort(t *testing.T) {
	ps, _ := passthroughProxy(t, "https://archive.example.test:80")

	r := proxyModeRequest(http.MethodGet, "archive.example.test:80", "/repo/InRelease")
	if rule := ps.handleExternalURLs(r); rule == nil {
		t.Fatal("pinned-port entry did not match")
	}
	if r.URL.Scheme != "https" {
		t.Errorf("scheme = %q, want https", r.URL.Scheme)
	}
	if r.URL.Host != "archive.example.test:80" {
		t.Errorf("host = %q, want the pinned port kept", r.URL.Host)
	}
}

// The default-port drop still applies when the entry pinned nothing.
func TestPassthroughStillDropsInheritedDefaultPort(t *testing.T) {
	ps, _ := passthroughProxy(t, "https://secure.example.test")

	for _, authority := range []string{"secure.example.test:80", "secure.example.test:443"} {
		r := proxyModeRequest(http.MethodGet, authority, "/repo/InRelease")
		if rule := ps.handleExternalURLs(r); rule == nil {
			t.Fatalf("%s: entry did not match", authority)
		}
		if r.URL.Host != "secure.example.test" {
			t.Errorf("%s: host = %q, want the inherited default port dropped", authority, r.URL.Host)
		}
	}
}

// An IPv6 origin must stay bracketed in the upstream URL. SplitHostPort hands
// back a bare address, and url.URL.Host without brackets reads the tail of it
// as a port: "[2001:db8::1]:443" became "2001:db8::1", serialising to
// https://2001:db8::1/... and dialling port 1.
func TestPassthroughKeepsIPv6Bracketed(t *testing.T) {
	ps, _ := passthroughProxy(t, "https://[2001:db8::1]")

	for _, authority := range []string{"[2001:db8::1]", "[2001:db8::1]:443", "[2001:db8::1]:80"} {
		r := proxyModeRequest(http.MethodGet, authority, "/repo/InRelease")
		if rule := ps.handleExternalURLs(r); rule == nil {
			t.Fatalf("%s: IPv6 entry did not match", authority)
		}
		if r.URL.Host != "[2001:db8::1]" {
			t.Errorf("%s: URL.Host = %q, want the address bracketed", authority, r.URL.Host)
		}
		if got, want := r.URL.String(), "https://[2001:db8::1]/repo/InRelease"; got != want {
			t.Errorf("%s: URL = %q, want %q", authority, got, want)
		}
	}
}

// A non-default port on an IPv6 origin is left alone, brackets and all.
func TestPassthroughKeepsIPv6WithPinnedPort(t *testing.T) {
	ps, _ := passthroughProxy(t, "https://[2001:db8::1]:8443")

	r := proxyModeRequest(http.MethodGet, "[2001:db8::1]:8443", "/repo/InRelease")
	if rule := ps.handleExternalURLs(r); rule == nil {
		t.Fatal("pinned-port IPv6 entry did not match")
	}
	if got, want := r.URL.String(), "https://[2001:db8::1]:8443/repo/InRelease"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestStripDefaultPort(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"example.test", "example.test"},
		{"example.test:80", "example.test"},
		{"example.test:443", "example.test"},
		{"example.test:8080", "example.test:8080"},
		{"[2001:db8::1]", "[2001:db8::1]"},
		{"[2001:db8::1]:80", "[2001:db8::1]"},
		{"[2001:db8::1]:443", "[2001:db8::1]"},
		{"[2001:db8::1]:8443", "[2001:db8::1]:8443"},
	} {
		if got := stripDefaultPort(tt.in); got != tt.want {
			t.Errorf("stripDefaultPort(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
