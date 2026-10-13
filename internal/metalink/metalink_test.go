package metalink

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/resource"
)

func document(data, mirrors string) string {
	h256 := sha256.Sum256([]byte(data))
	h512 := sha512.Sum512([]byte(data))
	return fmt.Sprintf(`<metalink xmlns="%s" xmlns:mm0="%s" version="3.0"><files><file name="repomd.xml"><mm0:timestamp>1791315727</mm0:timestamp><size>%d</size><verification><hash type="sha256">%x</hash><hash type="sha512">%x</hash></verification><resources>%s</resources></file></files></metalink>`, namespace, timestampNamespace, len(data), h256, h512, mirrors)
}
func resourceURL(base string, preference int) string {
	return fmt.Sprintf(`<url protocol="http" type="http" preference="%d">%s/repodata/repomd.xml</url>`, preference, base)
}
func localController(t *testing.T, server *httptest.Server, budget ...*resource.Manager) *Controller {
	t.Helper()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	c, err := New(&Config{PublicURL: "http://proxy.example:3142", SourceURL: server.URL + "/source", AllowedMirrorHosts: []string{u.Hostname()}}, &access.UpstreamConfig{Enabled: true, AllowedHosts: []string{u.Hostname()}, AllowedPorts: []int{port}, AllowCIDRs: []string{"127.0.0.0/8"}}, budget...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// This fixture is a shortened mirrors.centos.org response captured for
// centos-baseos-9-stream/x86_64 on 2026-10-13; its original namespace and hashes
// reproduce the MirrorManager XML structure rather than a namespace-free mock.
func TestParseCentOSStreamNamespaces(t *testing.T) {
	c, err := New(&Config{PublicURL: "http://proxy.example:3142", AllowedMirrorHosts: []string{"ftp.udx.icscoe.jp"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := `<metalink version="3.0" xmlns="http://www.metalinker.org/" xmlns:mm0="http://fedorahosted.org/mirrormanager"><files><file name="repomd.xml"><mm0:timestamp>1791315727</mm0:timestamp><size>4006</size><verification><hash type="md5">27cbab0e577d532803b44b06b94c8a02</hash><hash type="sha1">6b8ac85492130f461c8a778c01df4835a0a1a228</hash><hash type="sha256">676439b196665c004457874d63aedbc46092b0c56cde92d278f7142b37c9eb62</hash><hash type="sha512">66016e14fac2af7d431edec7d3b856a15b0420c482b3fb18419fa2d59578333d73a25eebbdd9e09189f19bf077a5adfabf1330941a81ea0a1623a89c0bc3f833</hash></verification><resources maxconnections="1"><url protocol="https" type="https" preference="100">https://ftp.udx.icscoe.jp/Linux/CentOS-stream/9-stream/BaseOS/x86_64/os/repodata/repomd.xml</url></resources></file></files></metalink>`
	m, err := c.parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if m.size != 4006 || m.timestamp != "1791315727" || len(m.hashes) != 4 || len(m.mirrors) != 1 {
		t.Fatalf("unexpected parsed metadata: %+v", m)
	}
	out := c.render(&snapshot{token: strings.Repeat("a", 64), metadata: m})
	reparsed, err := c.parse(out)
	// The generated proxy host is intentionally outside the origin allowlist.
	if err == nil || reparsed != nil {
		t.Fatal("generated proxy URL accidentally authorized as a mirror")
	}
	if !strings.Contains(string(out), m.hashes[2].Value) || !strings.Contains(string(out), "1791315727") || !strings.Contains(string(out), "http://proxy.example:3142/metalink/repos/") {
		t.Fatal("generated XML lost snapshot fields")
	}
}

func TestRejectMalformedMetadataAndUnsafeResources(t *testing.T) {
	c, _ := New(&Config{PublicURL: "http://proxy.example", AllowedMirrorHosts: []string{"mirror.example"}}, nil)
	good := document("repomd", resourceURL("http://mirror.example/repo", 100))
	for name, data := range map[string]string{
		"namespace":         strings.Replace(good, namespace, "urn:wrong", 1),
		"duplicate file":    strings.Replace(good, "</files>", strings.Split(strings.Split(good, "<files>")[1], "</files>")[0]+"</files>", 1),
		"duplicate hash":    strings.Replace(good, "</verification>", `<hash type="sha256">`+strings.Repeat("0", 64)+`</hash></verification>`, 1),
		"no strong hash":    strings.ReplaceAll(strings.ReplaceAll(good, "sha256", "unknown"), "sha512", "unknown2"),
		"deep":              strings.Replace(good, "<resources>", "<resources>"+strings.Repeat("<x>", 17), 1),
		"doctype":           "<!DOCTYPE metalink [<!ENTITY test 'expanded'>]>" + good,
		"trailing document": good + good,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.parse([]byte(data)); err == nil {
				t.Fatal("accepted malformed XML")
			}
		})
	}
	for _, unsafe := range []string{"http://user:secret@mirror.example/repo", "http://mirror.example/a/../repo", "http://mirror.example/a%2frepo", "http://mirror.example/a%5crepo", "http://mirror.example/repo?token=secret", "http://other.example/repo", "file:///repo", "http://mirror.example/repo#fragment"} {
		t.Run(unsafe, func(t *testing.T) {
			if _, err := c.parse([]byte(document("repomd", resourceURL(unsafe, 100)))); err == nil {
				t.Fatal("accepted unsafe resource")
			}
		})
	}
}

func TestQueryAndPublicRequestValidation(t *testing.T) {
	for _, query := range []string{"repo=r&arch=x86_64&protocol=https,http&countme=4", "repo=r&arch=a"} {
		if _, err := parseQuery(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"repo=r&repo=s&arch=a", "repo=r&arch=a&protocol=ftp", "repo=r&arch=a&protocol=https,https", "repo=r&arch=a&countme=0", "repo=r&arch=a&extra=x", "repo=../x&arch=a", "repo=r"} {
		if _, err := parseQuery(query); err == nil {
			t.Fatalf("accepted query %q", query)
		}
	}
	for _, name := range []string{"Authorization", "Cookie", "X-Token", "X-Tenant-ID", "Traceparent"} {
		r := httptest.NewRequest("GET", "http://proxy/metalink?repo=r&arch=a", nil)
		r.Header[name] = []string{""}
		if publicRequest(r) {
			t.Fatalf("accepted header %s", name)
		}
	}
	r := httptest.NewRequest("GET", "http://proxy/metalink", nil)
	r.Body = io.NopCloser(strings.NewReader(""))
	if !publicRequest(r) || r.Body != http.NoBody {
		t.Fatal("bodyless Fiber adapter request rejected")
	}
	r = httptest.NewRequest("GET", "http://proxy/metalink", nil)
	r.Body = io.NopCloser(strings.NewReader("unexpected"))
	if publicRequest(r) {
		t.Fatal("unframed body accepted")
	}
	r = httptest.NewRequest("GET", "http://proxy/metalink", nil)
	r.Trailer = http.Header{"X-Token": []string{""}}
	if publicRequest(r) {
		t.Fatal("trailer accepted")
	}
}

func TestPublicResponseUsesEveryCacheControlValue(t *testing.T) {
	for _, h := range []http.Header{{"Cache-Control": []string{"public,max-age=100", "PRIVATE"}}, {"Cache-Control": []string{"public", "no-store"}}, {"Set-Cookie": []string{""}}, {"Vary": []string{"Accept"}}} {
		if publicResponse(h) {
			t.Fatalf("accepted non-public headers: %v", h)
		}
		cacheHeaders(h)
		if h.Get("Cache-Control") != "no-store" {
			t.Fatal("privacy header lost")
		}
	}
}

func TestVerifiedFallbackRefreshAndCanonicalIdentity(t *testing.T) {
	const data = "<repomd>snapshot</repomd>"
	var server *httptest.Server
	var sourceCalls atomic.Int32
	var generation atomic.Int32
	var changed atomic.Bool
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/source":
			sourceCalls.Add(1)
			if r.URL.Query().Get("countme") != "4" && sourceCalls.Load() == 1 {
				t.Error("countme not forwarded")
			}
			mirrors := resourceURL(server.URL+"/dead", 100) + resourceURL(server.URL+"/good", 99)
			if generation.Load() > 0 {
				mirrors = resourceURL(server.URL+"/new", 100)
			}
			_, _ = io.WriteString(w, document(data, mirrors))
		case strings.HasPrefix(r.URL.Path, "/dead/"):
			w.WriteHeader(503)
		case strings.HasSuffix(r.URL.Path, "/repodata/repomd.xml"):
			if changed.Load() {
				_, _ = io.WriteString(w, "changed")
			} else {
				_, _ = io.WriteString(w, data)
			}
		default:
			_, _ = io.WriteString(w, "rpm payload")
		}
	}))
	defer server.Close()
	c := localController(t, server)
	q, _ := parseQuery("repo=centos-baseos-9-stream&arch=x86_64&protocol=http&countme=4")
	s, err := c.get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.metadata.mirrors[0].base.Path, "/good/") {
		t.Fatal("preferred failed mirror was selected")
	}
	q.Del("countme")
	same, err := c.get(context.Background(), q)
	if err != nil || same.token != s.token || sourceCalls.Load() != 1 {
		t.Fatal("countme fragmented repository identity")
	}
	var canonical []string
	transport := c.WrapTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("synthetic request escaped metalink transport")
		return nil, nil
	}))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canonical = append(canonical, r.URL.String())
		res, err := transport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	})
	serve := func(token, relative string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c.Handle(w, httptest.NewRequest("GET", "http://untrusted-host/metalink/repos/"+token+"/"+relative, nil), next)
		return w
	}
	if w := serve(s.token, "repodata/repomd.xml"); w.Code != 200 || w.Body.String() != data {
		t.Fatalf("repomd failed: %d %s", w.Code, w.Body.String())
	}
	generation.Store(1)
	c.Refresh()
	newer, err := c.get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if newer.token == s.token {
		t.Fatal("new mirror generation reused old issued route")
	}
	if w := serve(newer.token, "repodata/repomd.xml"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if canonical[0] != canonical[1] {
		t.Fatal("equivalent mirrored snapshot fragmented artifact cache")
	}
	if w := serve(s.token, "Packages/pkg.rpm"); w.Code != 200 || w.Body.String() != "rpm payload" {
		t.Fatal("old issued token stopped working after refresh")
	}
	changed.Store(true)
	if w := serve(newer.token, "repodata/repomd.xml"); w.Code != 502 {
		t.Fatal("changed repomd delivered under verified digest")
	}
	if w := serve(newer.token, "Packages/pkg.rpm"); w.Code != 502 {
		t.Fatal("changed snapshot authorized package fallback")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSingleflightExpiryCapacityAndFailedRefresh(t *testing.T) {
	const data = "repomd"
	var calls atomic.Int32
	var invalid atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/source" {
			calls.Add(1)
			if invalid.Load() {
				w.WriteHeader(503)
				return
			}
			_, _ = io.WriteString(w, document(data, resourceURL(server.URL+"/repo", 100)))
			return
		}
		_, _ = io.WriteString(w, data)
	}))
	defer server.Close()
	c := localController(t, server)
	c.cfg.MaxRepositories = 1
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, now.Load()) }
	q, _ := parseQuery("repo=r&arch=a&protocol=http")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.get(context.Background(), q); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent source requests=%d", calls.Load())
	}
	other, _ := parseQuery("repo=other&arch=a&protocol=http")
	if _, err := c.get(context.Background(), other); err != ErrCapacity {
		t.Fatal("repository cap not enforced")
	}
	now.Add(int64(601 * time.Second))
	invalid.Store(true)
	if _, err := c.get(context.Background(), q); err == nil {
		t.Fatal("stale Metalink used after failed refresh")
	}
	if calls.Load() != 2 {
		t.Fatal("expired source not retried")
	}
}

func TestResourceAdmissionAndPrivateDestination(t *testing.T) {
	manager, err := resource.New(resource.Config{MaxObjectBytes: 8 << 20, TotalTempBytes: 16, MaxActiveDownloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("origin reached without byte admission") }))
	defer server.Close()
	c := localController(t, server, manager)
	q, _ := parseQuery("repo=r&arch=a")
	if _, err := c.get(context.Background(), q); err == nil {
		t.Fatal("byte admission not enforced")
	}
	stats := manager.Stats()
	if stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
		t.Fatalf("resource leases leaked: %+v", stats)
	}
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	blocked, err := New(&Config{PublicURL: "http://proxy.example", SourceURL: server.URL + "/source", AllowedMirrorHosts: []string{u.Hostname()}}, &access.UpstreamConfig{Enabled: true, AllowedHosts: []string{u.Hostname()}, AllowedPorts: []int{port}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.get(context.Background(), q); err == nil {
		t.Fatal("private destination reached without explicit CIDR")
	}
}

func TestSHA512OnlyMetadata(t *testing.T) {
	c, _ := New(&Config{PublicURL: "http://proxy.example", AllowedMirrorHosts: []string{"mirror.example"}}, nil)
	data := "repomd"
	sum := sha512.Sum512([]byte(data))
	doc := fmt.Sprintf(`<metalink xmlns="%s" version="3.0"><files><file name="repomd.xml"><size>%d</size><verification><hash type="sha512">%s</hash></verification><resources>%s</resources></file></files></metalink>`, namespace, len(data), hex.EncodeToString(sum[:]), resourceURL("http://mirror.example/repo", 100))
	m, err := c.parse([]byte(doc))
	if err != nil || !m.verify([]byte(data)) {
		t.Fatalf("SHA512-only snapshot rejected: %v", err)
	}
}

func TestSourceFreshnessAndSnapshotExpiry(t *testing.T) {
	const data = "repomd"
	var calls atomic.Int32
	var noCache atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/source" {
			calls.Add(1)
			if noCache.Load() {
				w.Header().Set("Cache-Control", "no-cache, no-store")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=60")
				w.Header().Set("Age", "30")
			}
			_, _ = io.WriteString(w, document(data, resourceURL(server.URL+"/repo", 100)))
			return
		}
		_, _ = io.WriteString(w, data)
	}))
	defer server.Close()
	c := localController(t, server)
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, now.Load()) }
	q, _ := parseQuery("repo=r&arch=a&protocol=http")
	s, err := c.get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.repos[q.Encode()].expires.Sub(c.now()); got != 30*time.Second {
		t.Fatalf("source max-age/Age not respected: %v", got)
	}
	now.Store(s.expires.Add(-10 * time.Second).UnixNano())
	c.Refresh()
	if _, err = c.get(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if c.repos[q.Encode()].expires.After(s.expires) {
		t.Fatal("discovery outlived immutable route")
	}
	now.Add(int64(11 * time.Second))
	newer, err := c.get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if !newer.expires.After(c.now()) || newer == s {
		t.Fatal("expired route was reused")
	}
	noCache.Store(true)
	c.Refresh()
	before := calls.Load()
	for i := 0; i < 2; i++ {
		if _, err = c.get(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != before+2 {
		t.Fatal("source no-cache reused current discovery")
	}
}

func TestMetadataPrivacyAndArtifactRevalidation(t *testing.T) {
	const data = "repomd"
	var private atomic.Bool
	var redirect atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/source" {
			_, _ = io.WriteString(w, document(data, resourceURL(server.URL+"/repo", 100)))
			return
		}
		if strings.HasSuffix(r.URL.Path, "repomd.xml") {
			if r.Method != "GET" || r.Header.Get("Range") != "" {
				t.Error("repomd verification used partial request")
			}
			w.Header().Set("Cache-Control", "no-cache, max-age=0")
			if private.Load() {
				w.Header().Add("Cache-Control", "private")
			}
			_, _ = io.WriteString(w, data)
			return
		}
		if redirect.Load() {
			w.Header().Set("Location", server.URL+"/unexpected")
			w.WriteHeader(302)
			return
		}
		w.Header().Set("Cache-Control", "public,max-age=0")
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(304)
			return
		}
		_, _ = io.WriteString(w, "package")
	}))
	defer server.Close()
	c := localController(t, server)
	q, _ := parseQuery("repo=r&arch=a&protocol=http")
	s, err := c.get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	transport := c.WrapTransport(roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("escaped route"); return nil, nil }))
	var response *http.Response
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err = transport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		for k, v := range response.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
	request := func(method, relative string, conditional bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://proxy/metalink/repos/"+s.token+"/"+relative, nil)
		if conditional {
			r.Header.Set("If-None-Match", `"v1"`)
		}
		if method == "HEAD" {
			r.Header.Set("Range", "bytes=0-1")
		}
		w := httptest.NewRecorder()
		c.Handle(w, r, next)
		return w
	}
	if w := request("HEAD", "repodata/repomd.xml", false); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-cache, max-age=0" {
		t.Fatalf("verified HEAD metadata freshness lost: %d %v", w.Code, w.Header())
	}
	if w := request("GET", "Packages/pkg.rpm", true); w.Code != 304 || w.Header().Get("Cache-Control") != "public,max-age=0" {
		t.Fatalf("304 was rejected or freshened: %d %v", w.Code, w.Header())
	}
	redirect.Store(true)
	if w := request("GET", "Packages/pkg.rpm", false); w.Code != 502 {
		t.Fatal("artifact redirect accepted")
	}
	private.Store(true)
	if w := request("GET", "repodata/repomd.xml", false); w.Code != 502 {
		t.Fatal("private metadata delivered")
	}
}

func TestConfigurationAndRoutingValidation(t *testing.T) {
	if c, err := New(nil, nil); c != nil || err != nil {
		t.Fatal("nil configuration not disabled")
	}
	base := Config{PublicURL: "http://proxy.example", AllowedMirrorHosts: []string{"mirror.example"}}
	for name, change := range map[string]func(*Config){
		"public path":              func(c *Config) { c.PublicURL = "http://proxy.example/path" },
		"public credentials":       func(c *Config) { c.PublicURL = "http://user:pass@proxy.example" },
		"source query":             func(c *Config) { c.SourceURL = "https://source.example/metalink?repo=r" },
		"missing hosts":            func(c *Config) { c.AllowedMirrorHosts = nil },
		"wildcard host":            func(c *Config) { c.AllowedMirrorHosts = []string{"*.example"} },
		"invalid host label":       func(c *Config) { c.AllowedMirrorHosts = []string{"a..example"} },
		"invalid refresh":          func(c *Config) { c.RefreshSeconds = -1 },
		"invalid timeout":          func(c *Config) { c.TimeoutSeconds = 121 },
		"invalid XML limit":        func(c *Config) { c.MaxXMLBytes = 8 },
		"invalid repository limit": func(c *Config) { c.MaxRepositories = 65 },
		"invalid repomd limit":     func(c *Config) { c.MaxRepomdBytes = 33 << 20 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			change(&cfg)
			if _, err := New(&cfg, nil); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	if _, err := New(&base, &access.UpstreamConfig{Enabled: true, AllowedHosts: []string{"unrelated.example"}}); err == nil {
		t.Fatal("source bypassed configured host restriction")
	}
	if _, err := New(&base, &access.UpstreamConfig{AllowedHosts: []string{"mirrors.centos.org"}}); err == nil {
		t.Fatal("disabled policy restrictions ignored")
	}
	c, _ := New(&base, nil)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid route reached cache") })
	for _, target := range []string{"http://proxy/metalink/repos/unknown/Packages/x.rpm", "http://proxy/metalink/repos/" + strings.Repeat("a", 64) + "/Packages/x.rpm?secret=x", "http://proxy/metalink/other", "http://proxy/metalink/repos/" + strings.Repeat("a", 64) + "/../x", "http://proxy/metalink/repos/" + strings.Repeat("a", 64) + "/a%2fb"} {
		w := httptest.NewRecorder()
		if !c.Handle(w, httptest.NewRequest("GET", target, nil), next) || w.Code < 400 {
			t.Fatal("invalid route accepted")
		}
	}
	w := httptest.NewRecorder()
	if c.Handle(w, httptest.NewRequest("GET", "http://proxy/centos/Packages/x.rpm", nil), next) {
		t.Fatal("normal distro path intercepted")
	}
	w = httptest.NewRecorder()
	c.Handle(w, httptest.NewRequest("POST", "http://proxy/metalink", nil), next)
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal("method gate missing")
	}
	var otherCalled bool
	transport := c.WrapTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		otherCalled = true
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}))
	otherResponse, err := transport.RoundTrip(httptest.NewRequest("GET", "http://other.example/file", nil))
	if otherResponse != nil {
		_ = otherResponse.Body.Close()
	}
	if err != nil || !otherCalled {
		t.Fatal("normal transport changed")
	}
	deniedResponse, err := transport.RoundTrip(httptest.NewRequest("GET", "http://"+canonicalHost+"/fabricated", nil))
	if deniedResponse != nil {
		_ = deniedResponse.Body.Close()
	}
	if err == nil {
		t.Fatal("naked synthetic host allowed")
	}
	var disabled *Controller
	disabled.Refresh()
	if disabled.Handle(w, httptest.NewRequest("GET", "http://proxy/metalink", nil), next) {
		t.Fatal("nil controller handled request")
	}
}

func TestRefreshRefusesPendingFetchThenRetryPublishes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/source" {
			n := calls.Add(1)
			if n == 1 {
				close(started)
				<-release
			}
			_, _ = io.WriteString(w, document("repomd", resourceURL(server.URL+"/repo", 100)))
			return
		}
		_, _ = io.WriteString(w, "repomd")
	}))
	defer server.Close()
	c := localController(t, server)
	q, _ := parseQuery("repo=r&arch=a&protocol=http")
	result := make(chan error, 1)
	go func() { _, err := c.get(context.Background(), q); result <- err }()
	<-started
	c.Refresh()
	close(release)
	if err := <-result; err == nil {
		t.Fatal("pre-refresh pending fetch was published")
	}
	if len(c.snapshots) != 0 || len(c.repos) != 0 {
		t.Fatal("invalidated pending state remained reusable")
	}
	s, err := c.get(context.Background(), q)
	if err != nil || s == nil || calls.Load() != 2 {
		t.Fatalf("retry did not discover a fresh snapshot: %v", err)
	}
}
