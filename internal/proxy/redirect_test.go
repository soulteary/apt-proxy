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
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/passthrough"
	"github.com/soulteary/apt-proxy/internal/storage"
)

// An allowlisted origin, the CDN host it redirects to, which is not
// allowlisted, and a package path, shaped after pkgs.k8s.io.
const (
	redirectOrigin    = "pkgs.example.org"
	redirectCDN       = "cdn.example.net"
	redirectPackage   = "/core/deb/amd64/kubectl_1.31.0_amd64.deb"
	redirectOriginURL = "https://" + redirectOrigin + redirectPackage
	redirectCDNURL    = "https://" + redirectCDN + "/repositories" + redirectPackage
)

// scriptedAnswer describes the response to one URL of a scriptedNetwork.
type scriptedAnswer struct {
	status int
	header http.Header
	body   string
	// reader replaces body when a test has to observe the response body.
	reader io.ReadCloser
}

// scriptedNetwork stands in for the network. It answers a request with what
// was registered for its URL, query excluded, and records every request.
type scriptedNetwork struct {
	mu      sync.Mutex
	seen    []*http.Request
	answers map[string]scriptedAnswer
}

func newScriptedNetwork() *scriptedNetwork {
	return &scriptedNetwork{answers: make(map[string]scriptedAnswer)}
}

func (n *scriptedNetwork) on(target string, answer scriptedAnswer) *scriptedNetwork {
	n.answers[target] = answer
	return n
}

func (n *scriptedNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.mu.Lock()
	n.seen = append(n.seen, r.Clone(context.Background()))
	answer, known := n.answers[r.URL.Scheme+"://"+r.URL.Host+r.URL.EscapedPath()]
	n.mu.Unlock()
	if !known {
		return nil, fmt.Errorf("scripted network: %s is unreachable", r.URL.Host)
	}
	body := answer.reader
	if body == nil {
		body = io.NopCloser(strings.NewReader(answer.body))
	}
	return &http.Response{
		StatusCode: answer.status, Header: answer.header.Clone(), Body: body,
		ContentLength: int64(len(answer.body)), Request: r,
	}, nil
}

func (n *scriptedNetwork) requests() []*http.Request {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.seen)
}

// urls lists the URLs requested so far, in order.
func (n *scriptedNetwork) urls() []string {
	var urls []string
	for _, r := range n.requests() {
		urls = append(urls, r.URL.String())
	}
	return urls
}

// redirectTo answers with a redirect to location; an empty location leaves
// the header out.
func redirectTo(status int, location string) scriptedAnswer {
	header := http.Header{}
	if location != "" {
		header.Set("Location", location)
	}
	return scriptedAnswer{status: status, header: header, body: "moved"}
}

// content answers 200 with body and the given header fields, as name, value pairs.
func content(body string, fields ...string) scriptedAnswer {
	header := http.Header{"Content-Length": {strconv.Itoa(len(body))}}
	for i := 0; i+1 < len(fields); i += 2 {
		header.Set(fields[i], fields[i+1])
	}
	return scriptedAnswer{status: http.StatusOK, header: header, body: body}
}

func redirectAllowlist(t *testing.T, entries ...string) *passthrough.List {
	t.Helper()
	list, err := passthrough.Parse(entries)
	if err != nil {
		t.Fatalf("passthrough.Parse(%v): %v", entries, err)
	}
	return list
}

// redirectProxy builds a proxy whose upstream is network.
func redirectProxy(t *testing.T, network http.RoundTripper, follow bool, entries ...string) *PackageStruct {
	t.Helper()
	st := newTestState()
	st.SetProxyMode(distro.TypeAllDistros)
	ps, err := NewPackageStruct(Options{
		State: st, Registry: newTestRegistry(), CacheDir: t.TempDir(), Logger: logger.Default(),
		Mode: distro.TypeAllDistros, Passthrough: redirectAllowlist(t, entries...), FollowRedirects: follow,
		TransportOverride: network,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	return ps
}

// requestThrough sends a request for target, an absolute URL as an HTTP proxy receives
// it, through ps.
func requestThrough(ps *PackageStruct, method, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for name, values := range header {
		req.Header[name] = values
	}
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)
	return rec
}

// passthroughRequest is a request as the follower receives it: routed to an
// allowlisted origin and carrying the passthrough rule.
func passthroughRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.WithValue(context.Background(), cacheRuleKey{}, passthroughRule), method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// redirectLog returns a logger and the sink it writes to.
func redirectLog() (*logger.Logger, *lockedBuffer) {
	sink := &lockedBuffer{}
	return logger.New(logger.Config{Level: logger.DebugLevel, Output: sink, Format: logger.FormatJSON}), sink
}

// Both ways a client names a third-party origin lead to the redirected file,
// whichever redirect status the origin uses.
func TestRedirectFromPassthroughOriginIsFollowed(t *testing.T) {
	forms := []struct{ name, target string }{
		{"proxy mode", "http://" + redirectOrigin + redirectPackage},
		{"marker", "http://apt-proxy.example:3142/HTTPS///" + redirectOrigin + redirectPackage},
	}
	statuses := []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	}
	for _, form := range forms {
		for _, status := range statuses {
			t.Run(fmt.Sprintf("%s/%d", form.name, status), func(t *testing.T) {
				network := newScriptedNetwork().
					on(redirectOriginURL, redirectTo(status, redirectCDNURL)).
					on(redirectCDNURL, content("package"))
				ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

				rec := requestThrough(ps, http.MethodGet, form.target, nil)

				if rec.Code != http.StatusOK || rec.Body.String() != "package" {
					t.Fatalf("status=%d body=%q, want the redirected file", rec.Code, rec.Body.String())
				}
				if location := rec.Header().Get("Location"); location != "" {
					t.Errorf("the client was given Location %q", location)
				}
				if got, want := network.urls(), []string{redirectOriginURL, redirectCDNURL}; !slices.Equal(got, want) {
					t.Fatalf("upstream requests = %v, want %v", got, want)
				}
				if host := network.requests()[1].Host; host != redirectCDN {
					t.Errorf("the redirect target was asked for Host %q, want %q", host, redirectCDN)
				}
			})
		}
	}
}

func TestRedirectIsFollowedForHEAD(t *testing.T) {
	network := newScriptedNetwork().
		on(redirectOriginURL, redirectTo(http.StatusFound, redirectCDNURL)).
		on(redirectCDNURL, content(""))
	ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

	rec := requestThrough(ps, http.MethodHead, "http://"+redirectOrigin+redirectPackage, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	requests := network.requests()
	if len(requests) != 2 || requests[1].Method != http.MethodHead || requests[1].URL.String() != redirectCDNURL {
		t.Fatalf("upstream requests = %v, want a HEAD for the origin and one for the target", network.urls())
	}
}

// cachedProxyServer puts the HTTP cache in front of ps, as the daemon does.
func cachedProxyServer(t *testing.T, ps *PackageStruct) *httptest.Server {
	t.Helper()
	backend, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := httpcache.NewHandlerWithOptions(&storage.VerifiedCache{ExtendedCache: backend}, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached
	server := httptest.NewServer(ps)
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	return server
}

// The point of following: the file is stored under the URL the client asked
// for, so a second request contacts neither the origin nor the CDN.
func TestRedirectedResponseIsCachedUnderRequestedURL(t *testing.T) {
	network := newScriptedNetwork().
		on(redirectOriginURL, redirectTo(http.StatusFound, redirectCDNURL)).
		on(redirectCDNURL, content("package", "Cache-Control", "public, max-age=3600"))
	server := cachedProxyServer(t, redirectProxy(t, network, true, "https://"+redirectOrigin))

	for _, want := range []string{"MISS", "HIT"} {
		status, body, header := immutableTestGet(t, server, "/HTTPS///"+redirectOrigin+redirectPackage, nil)
		if status != http.StatusOK || body != "package" || header.Get("X-Cache") != want {
			t.Fatalf("status=%d body=%q X-Cache=%q, want 200 %q %s", status, body, header.Get("X-Cache"), "package", want)
		}
	}
	if got, want := network.urls(), []string{redirectOriginURL, redirectCDNURL}; !slices.Equal(got, want) {
		t.Fatalf("upstream requests = %v, want %v", got, want)
	}
}

// With the switch off the redirect reaches the client, as before.
func TestRedirectIsPassedToClientWhenFollowingIsOff(t *testing.T) {
	network := newScriptedNetwork().
		on(redirectOriginURL, redirectTo(http.StatusFound, redirectCDNURL)).
		on(redirectCDNURL, content("package"))
	ps := redirectProxy(t, network, false, "https://"+redirectOrigin)

	rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, nil)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != redirectCDNURL {
		t.Fatalf("status=%d Location=%q, want the origin's redirect", rec.Code, rec.Header().Get("Location"))
	}
	if got := network.urls(); !slices.Equal(got, []string{redirectOriginURL}) {
		t.Fatalf("upstream requests = %v, want the origin only", got)
	}
}

// Following is limited to passthrough origins. A mirror's redirect is passed on.
func TestMirrorRedirectIsNotFollowed(t *testing.T) {
	const (
		mirrorURL = "http://mirrors.example.com/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb"
		location  = "http://elsewhere.example.com/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb"
	)
	network := newScriptedNetwork().
		on(mirrorURL, redirectTo(http.StatusFound, location)).
		on(location, content("package"))
	ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

	rec := requestThrough(ps, http.MethodGet, "http://apt-proxy.example:3142/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb", nil)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != location {
		t.Fatalf("status=%d Location=%q, want the mirror's redirect", rec.Code, rec.Header().Get("Location"))
	}
	if got := network.urls(); !slices.Equal(got, []string{mirrorURL}) {
		t.Fatalf("upstream requests = %v, want the mirror only", got)
	}
}

// Credentials the client sent for the origin stay with the origin. The
// response is still treated as the answer to a request that carried them.
func TestRedirectKeepsClientCredentialsWithOrigin(t *testing.T) {
	const (
		moved = "https://" + redirectOrigin + "/moved" + redirectPackage
		final = "https://" + redirectOrigin + "/final" + redirectPackage
	)
	network := newScriptedNetwork().
		on(redirectOriginURL, redirectTo(http.StatusFound, "/moved"+redirectPackage)).
		on(moved, redirectTo(http.StatusFound, redirectCDNURL)).
		on(redirectCDNURL, redirectTo(http.StatusTemporaryRedirect, final)).
		on(final, content("package", "Cache-Control", "public, max-age=3600"))
	ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

	rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, http.Header{
		"Authorization": {"Bearer client-token"},
		"Cookie":        {"session=client"},
	})

	if rec.Code != http.StatusOK || rec.Body.String() != "package" {
		t.Fatalf("status=%d body=%q, want the redirected file", rec.Code, rec.Body.String())
	}
	if got, want := network.urls(), []string{redirectOriginURL, moved, redirectCDNURL, final}; !slices.Equal(got, want) {
		t.Fatalf("upstream requests = %v, want %v", got, want)
	}
	for i, want := range []bool{true, true, false, false} {
		r := network.requests()[i]
		got := r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != ""
		if got != want {
			t.Errorf("request %d to %s: credentials sent = %t, want %t", i, r.URL.Host, got, want)
		}
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q: a response obtained with client credentials must stay out of the shared cache", cc)
	}
}

// A chain longer than the limit ends at the client with the next location
// spelled out, since the client asked for a different URL than the last hop.
func TestRedirectChainBeyondLimitIsHandedBack(t *testing.T) {
	network := newScriptedNetwork().on(redirectOriginURL, redirectTo(http.StatusFound, "https://"+redirectCDN+"/hop/1"))
	for i := 1; i <= maxRedirectHops+1; i++ {
		network.on("https://"+redirectCDN+"/hop/"+strconv.Itoa(i), redirectTo(http.StatusFound, strconv.Itoa(i+1)))
	}
	ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

	rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, nil)

	want := "https://" + redirectCDN + "/hop/" + strconv.Itoa(maxRedirectHops+1)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
		t.Fatalf("status=%d Location=%q, want 302 to %s", rec.Code, rec.Header().Get("Location"), want)
	}
	if got := len(network.requests()); got != maxRedirectHops+1 {
		t.Fatalf("%d upstream requests, want the origin and %d hops", got, maxRedirectHops)
	}
}

func TestRedirectLocationIsResolvedAgainstRedirectingURL(t *testing.T) {
	for _, tc := range []struct{ name, location, want string }{
		{"absolute path", "/pool/k/kubectl.deb", "https://" + redirectOrigin + "/pool/k/kubectl.deb"},
		{"relative path", "kubectl.deb", "https://" + redirectOrigin + "/core/deb/amd64/kubectl.deb"},
		{"scheme relative", "//" + redirectCDN + "/kubectl.deb", "https://" + redirectCDN + "/kubectl.deb"},
		{"query", "https://" + redirectCDN + "/kubectl.deb?expires=1&signature=abc", "https://" + redirectCDN + "/kubectl.deb?expires=1&signature=abc"},
		{"fragment", "https://" + redirectCDN + "/kubectl.deb#top", "https://" + redirectCDN + "/kubectl.deb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, _, _ := strings.Cut(tc.want, "?")
			network := newScriptedNetwork().
				on(redirectOriginURL, redirectTo(http.StatusFound, tc.location)).
				on(target, content("package"))
			ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

			rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, nil)

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", rec.Code)
			}
			if got := network.urls(); len(got) != 2 || got[1] != tc.want {
				t.Fatalf("upstream requests = %v, want the second to be %s", got, tc.want)
			}
		})
	}
}

// A redirect without a usable http or https location is the client's to
// interpret.
func TestRedirectWithoutUsableLocationIsPassedOn(t *testing.T) {
	for _, location := range []string{"", "ftp://" + redirectCDN + "/kubectl.deb", "mailto:admin@example.org", "http://[::1"} {
		t.Run(location, func(t *testing.T) {
			network := newScriptedNetwork().on(redirectOriginURL, redirectTo(http.StatusFound, location))
			ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

			rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, nil)

			if rec.Code != http.StatusFound || rec.Header().Get("Location") != location || rec.Body.String() != "moved" {
				t.Fatalf("status=%d Location=%q body=%q, want the origin's response", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			if got := network.urls(); !slices.Equal(got, []string{redirectOriginURL}) {
				t.Fatalf("upstream requests = %v, want the origin only", got)
			}
		})
	}
}

// The scheme of a hop follows the allowlist entry when the target is listed,
// and may not fall back from https to http when it is not.
func TestRedirectTargetScheme(t *testing.T) {
	for _, tc := range []struct {
		name     string
		entries  []string
		location string
		wantHop  string // empty: the redirect is not followed
	}{
		{"https to http outside the allowlist", []string{"https://" + redirectOrigin}, "http://" + redirectCDN + "/kubectl.deb", ""},
		{"https to an origin listed for http", []string{"https://" + redirectOrigin, "plain.example.org"}, "http://plain.example.org/kubectl.deb", "http://plain.example.org/kubectl.deb"},
		{"http location of an origin listed for https", []string{"https://" + redirectOrigin, "https://secure.example.org"}, "http://secure.example.org/kubectl.deb", "https://secure.example.org/kubectl.deb"},
		{"default port of an origin listed for https", []string{"https://" + redirectOrigin, "https://secure.example.org"}, "http://secure.example.org:80/kubectl.deb", "https://secure.example.org/kubectl.deb"},
		{"https to https outside the allowlist", []string{"https://" + redirectOrigin}, "https://" + redirectCDN + "/kubectl.deb", "https://" + redirectCDN + "/kubectl.deb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			network := newScriptedNetwork().on(redirectOriginURL, redirectTo(http.StatusFound, tc.location))
			if tc.wantHop != "" {
				network.on(tc.wantHop, content("package"))
			}
			ps := redirectProxy(t, network, true, tc.entries...)

			rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, nil)

			if tc.wantHop == "" {
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != tc.location {
					t.Fatalf("status=%d Location=%q, want the origin's redirect", rec.Code, rec.Header().Get("Location"))
				}
				if got := network.urls(); !slices.Equal(got, []string{redirectOriginURL}) {
					t.Fatalf("upstream requests = %v, want the origin only", got)
				}
				return
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", rec.Code)
			}
			if got := network.urls(); !slices.Equal(got, []string{redirectOriginURL, tc.wantHop}) {
				t.Fatalf("upstream requests = %v, want the origin and %s", got, tc.wantHop)
			}
		})
	}
}

// From an origin fetched over http, an http location is no downgrade.
func TestRedirectFromPlainOriginToPlainTarget(t *testing.T) {
	const origin, location = "http://" + redirectOrigin + redirectPackage, "http://" + redirectCDN + "/kubectl.deb"
	network := newScriptedNetwork().
		on(origin, redirectTo(http.StatusFound, location)).
		on(location, content("package"))
	ps := redirectProxy(t, network, true, redirectOrigin)

	rec := requestThrough(ps, http.MethodGet, origin, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := network.urls(); !slices.Equal(got, []string{origin, location}) {
		t.Fatalf("upstream requests = %v, want the origin and its target", got)
	}
}

type closeTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *closeTrackingBody) Close() error { b.closed.Store(true); return nil }

// A target that fails is an upstream failure, not a redirect for the client.
func TestRedirectTargetFailureIsUpstreamFailure(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader("moved")}
	network := newScriptedNetwork().on(redirectOriginURL, scriptedAnswer{
		status: http.StatusFound, header: http.Header{"Location": {redirectCDNURL}}, reader: body,
	})
	ps := redirectProxy(t, network, true, "https://"+redirectOrigin)

	rec := requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502", rec.Code)
	}
	if !body.closed.Load() {
		t.Error("the body of the redirect was left open")
	}
}

// Whatever the client asked of the origin, it asks of the target.
func TestRedirectForwardsRequestHeaders(t *testing.T) {
	network := newScriptedNetwork().
		on(redirectOriginURL, redirectTo(http.StatusFound, redirectCDNURL)).
		on(redirectCDNURL, content("package"))
	ps := redirectProxy(t, network, true, "https://"+redirectOrigin)
	sent := http.Header{
		"Range":         {"bytes=100-"},
		"If-None-Match": {`"abc"`},
		"User-Agent":    {"Debian APT-HTTP/1.3 (2.8.3)"},
	}

	requestThrough(ps, http.MethodGet, "http://"+redirectOrigin+redirectPackage, sent)

	requests := network.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream requests = %v, want two", network.urls())
	}
	for name, values := range sent {
		if got := requests[1].Header.Get(name); got != values[0] {
			t.Errorf("%s reached the target as %q, want %q", name, got, values[0])
		}
	}
}

// The follower leaves everything but passthrough fetches alone.
func TestRedirectFollowerAppliesToPassthroughFetchesOnly(t *testing.T) {
	network := newScriptedNetwork().
		on(redirectOriginURL, redirectTo(http.StatusFound, redirectCDNURL)).
		on(redirectCDNURL, content("package"))
	follower := newRedirectFollower(network, nil, nil, redirectAllowlist(t, "https://"+redirectOrigin), logger.Default())

	distribution, err := http.NewRequest(http.MethodGet, redirectOriginURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, req := range map[string]*http.Request{
		"no passthrough rule": distribution,
		"POST":                passthroughRequest(t, http.MethodPost, redirectOriginURL),
	} {
		res, err := follower.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusFound {
			t.Errorf("%s: status=%d, want the redirect", name, res.StatusCode)
		}
	}
	if got := len(network.requests()); got != 2 {
		t.Fatalf("%d upstream requests, want one per request", got)
	}
}

// The guard for targets outside the allowlist is the real one here: no
// address inside the host or its network is contacted, and the client
// receives the redirect.
func TestRedirectToInternalAddressIsNotFollowed(t *testing.T) {
	var internalRequests atomic.Int64
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalRequests.Add(1)
		_, _ = io.WriteString(w, "internal")
	}))
	defer internal.Close()

	for _, tc := range []struct{ location, reason string }{
		{internal.URL + "/secret", "port is not allowed"},
		{"http://127.0.0.1/secret", "address is not allowed"},
		{"http://[::1]/secret", "address is not allowed"},
		{"http://localhost/secret", "address is not allowed"},
		{"http://10.0.0.8/secret", "address is not allowed"},
		{"http://172.16.0.1/secret", "address is not allowed"},
		{"http://192.168.1.1/secret", "address is not allowed"},
		{"http://169.254.169.254/latest/meta-data/", "address is not allowed"},
		{"http://100.64.0.1/secret", "address is not allowed"},
		{"http://0.0.0.0/secret", "address is not allowed"},
		{"http://[fd00::1]/secret", "address is not allowed"},
		{"http://[fe80::1]/secret", "address is not allowed"},
		{"http://[::ffff:127.0.0.1]/secret", "address is not allowed"},
		{"https://" + redirectCDN + ":8443/kubectl.deb", "port is not allowed"},
	} {
		t.Run(tc.location, func(t *testing.T) {
			origin := newScriptedNetwork().on("http://"+redirectOrigin+redirectPackage, redirectTo(http.StatusFound, tc.location))
			// The test environment may route through a forward proxy, which
			// is a different refusal; see TestRedirectThroughForwardProxy.
			base := NewUpstreamTransport(false)
			base.Proxy = nil
			log, sink := redirectLog()
			follower := newRedirectFollower(origin, base, nil, redirectAllowlist(t, redirectOrigin), log)

			res, err := follower.RoundTrip(passthroughRequest(t, http.MethodGet, "http://"+redirectOrigin+redirectPackage))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode != http.StatusFound || res.Header.Get("Location") != tc.location || string(body) != "moved" {
				t.Fatalf("status=%d Location=%q body=%q, want the origin's redirect", res.StatusCode, res.Header.Get("Location"), body)
			}
			logs := sink.String()
			if !strings.Contains(logs, "was not followed") || !strings.Contains(logs, tc.reason) {
				t.Errorf("log does not name the refusal %q: %s", tc.reason, logs)
			}
			if !strings.Contains(logs, "add the host to passthrough") {
				t.Errorf("log does not say how to follow the redirect: %s", logs)
			}
		})
	}
	if got := internalRequests.Load(); got != 0 {
		t.Fatalf("the internal listener received %d requests", got)
	}
}

// Over real connections: the origin redirects to a second server, which is
// reached through the address-checking transport and answers the request.
func TestRedirectIsFollowedOverRealConnections(t *testing.T) {
	type hit struct{ path, host, ifNoneMatch string }
	hits := make(chan hit, 2)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- hit{r.URL.RequestURI(), r.Host, r.Header.Get("If-None-Match")}
		_, _ = io.WriteString(w, "package")
	}))
	defer cdn.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/repositories"+r.URL.Path+"?signature=abc", http.StatusFound)
	}))
	defer origin.Close()

	// PublicOnly would refuse the loopback listener, so this policy admits
	// it. The follower and the transports are the ones the daemon builds.
	_, port, err := net.SplitHostPort(strings.TrimPrefix(cdn.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	loopback, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowCIDRs: []string{"127.0.0.0/8"}, AllowedPorts: []int{number}})
	if err != nil {
		t.Fatal(err)
	}
	base := NewUpstreamTransport(true)
	base.Proxy = nil
	defer base.CloseIdleConnections()
	follower := newRedirectFollower(NewRetryableTransport(base), base, nil, redirectAllowlist(t, redirectOrigin), logger.Default())
	follower.public = NewRetryableTransport(loopback.Transport(base))

	for range 2 {
		req := passthroughRequest(t, http.MethodGet, origin.URL+redirectPackage)
		req.Header.Set("If-None-Match", `"abc"`)
		res, err := follower.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || string(body) != "package" {
			t.Fatalf("status=%d body=%q err=%v, want the redirected file", res.StatusCode, body, err)
		}
		if res.Request != req {
			t.Error("the response does not belong to the request that was made")
		}
		want := hit{"/repositories" + redirectPackage + "?signature=abc", strings.TrimPrefix(cdn.URL, "http://"), `"abc"`}
		if got := <-hits; got != want {
			t.Fatalf("the target received %+v, want %+v", got, want)
		}
	}
}

// A hop to an allowlisted origin uses the regular upstream chain, like a
// client request for that origin. Any other hop uses the address-checking one.
func TestRedirectHopTransport(t *testing.T) {
	const listed = "https://listed.example.org/kubectl.deb"
	for _, tc := range []struct {
		name, location string
		wantPublic     bool
	}{
		{"allowlisted target", listed, false},
		{"target outside the allowlist", redirectCDNURL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			regular := newScriptedNetwork().
				on(redirectOriginURL, redirectTo(http.StatusFound, tc.location)).
				on(tc.location, content("package"))
			public := newScriptedNetwork().on(tc.location, content("package"))
			follower := newRedirectFollower(regular, nil, nil, redirectAllowlist(t, "https://"+redirectOrigin, "https://listed.example.org"), logger.Default())
			follower.public = public

			res, err := follower.RoundTrip(passthroughRequest(t, http.MethodGet, redirectOriginURL))
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status=%d, want 200", res.StatusCode)
			}
			if got := slices.Contains(public.urls(), tc.location); got != tc.wantPublic {
				t.Errorf("hop through the address-checking transport = %t, want %t (regular: %v, address-checking: %v)",
					got, tc.wantPublic, regular.urls(), public.urls())
			}
		})
	}
}

// Through a forward proxy the address of a target cannot be checked, so only
// allowlisted targets are followed.
func TestRedirectThroughForwardProxy(t *testing.T) {
	const listed = "https://listed.example.org/kubectl.deb"
	viaProxy := func(*http.Request) (*url.URL, error) { return url.Parse("http://egress.example:3128") }
	direct := func(*http.Request) (*url.URL, error) { return nil, nil }

	for _, tc := range []struct {
		name     string
		proxy    func(*http.Request) (*url.URL, error)
		location string
		followed bool
	}{
		{"target outside the allowlist", viaProxy, redirectCDNURL, false},
		{"allowlisted target", viaProxy, listed, true},
		{"target excluded from the proxy", direct, redirectCDNURL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			network := newScriptedNetwork().
				on(redirectOriginURL, redirectTo(http.StatusFound, tc.location)).
				on(tc.location, content("package"))
			log, sink := redirectLog()
			follower := newRedirectFollower(network, nil, nil, redirectAllowlist(t, "https://"+redirectOrigin, "https://listed.example.org"), log)
			follower.egressProxy = tc.proxy

			res, err := follower.RoundTrip(passthroughRequest(t, http.MethodGet, redirectOriginURL))
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if followed := res.StatusCode == http.StatusOK; followed != tc.followed {
				t.Fatalf("status=%d, followed=%t, want %t", res.StatusCode, followed, tc.followed)
			}
			if !tc.followed && !strings.Contains(sink.String(), "add the host to passthrough") {
				t.Errorf("log does not say how to follow the redirect: %s", sink.String())
			}
		})
	}
}

// The operator's upstream_access policy applies to redirect targets as well.
func TestRedirectTargetIsSubjectToUpstreamAccessPolicy(t *testing.T) {
	const permitted = "https://permitted.example.net/kubectl.deb"
	policy, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedHosts: []string{redirectOrigin, "permitted.example.net"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		location string
		followed bool
	}{{redirectCDNURL, false}, {permitted, true}} {
		t.Run(tc.location, func(t *testing.T) {
			network := newScriptedNetwork().
				on(redirectOriginURL, redirectTo(http.StatusFound, tc.location)).
				on(tc.location, content("package"))
			log, sink := redirectLog()
			follower := newRedirectFollower(network, nil, policy, redirectAllowlist(t, "https://"+redirectOrigin), log)

			res, err := follower.RoundTrip(passthroughRequest(t, http.MethodGet, redirectOriginURL))
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if followed := res.StatusCode == http.StatusOK; followed != tc.followed {
				t.Fatalf("status=%d, followed=%t, want %t", res.StatusCode, followed, tc.followed)
			}
			if !tc.followed && !strings.Contains(sink.String(), "upstream_access does not allow the target") {
				t.Errorf("log does not name the policy: %s", sink.String())
			}
		})
	}
}

// How the follower is assembled decides which guarantees hold.
func TestNewRedirectFollowerTransports(t *testing.T) {
	list := redirectAllowlist(t, redirectOrigin)
	next := newScriptedNetwork()
	policy, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	supplied := newRedirectFollower(next, nil, nil, list, logger.Default())
	if supplied.public != http.RoundTripper(next) || supplied.egressProxy != nil {
		t.Error("a supplied transport must carry every hop itself")
	}

	base := NewUpstreamTransport(true)
	direct := newRedirectFollower(next, base, nil, list, logger.Default())
	if direct.public == http.RoundTripper(next) {
		t.Error("targets outside the allowlist must use the address-checking transport")
	}
	if direct.egressProxy == nil {
		t.Error("the forward proxy of the regular transport must be consulted")
	}

	guarded := newRedirectFollower(next, base, policy, list, logger.Default())
	if guarded.egressProxy != nil || guarded.policy != policy {
		t.Error("with upstream_access enabled every connection is direct and checked by the policy")
	}
}
