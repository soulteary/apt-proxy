package sharedcache

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyHandlerKeepsPrivateTrafficOutOfCache(t *testing.T) {
	p := &Policy{}
	for _, mode := range []string{"header", "body", "flush"} {
		t.Run(mode, func(t *testing.T) {
			cached := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("private request reached cache") })
			uncached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if w.(*privateWriter).Unwrap() == nil {
					t.Fatal("lost writer")
				}
				switch mode {
				case "header":
					w.WriteHeader(202)
				case "body":
					_, _ = io.WriteString(w, "private")
				case "flush":
					w.(http.Flusher).Flush()
				}
			})
			req := httptest.NewRequest("GET", "http://example/pkg", nil)
			req.Header.Set("Authorization", "secret")
			rec := httptest.NewRecorder()
			p.WrapHandler(cached, uncached).ServeHTTP(rec, req)
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response can be cached")
			}
		})
	}
	req := httptest.NewRequest("HEAD", "http://example/pkg", nil)
	for _, name := range []string{"X-Request-ID", "Connection", "Proxy-Connection", "Keep-Alive", "Host", "Content-Length"} {
		v := "keep-alive"
		if name == "Content-Length" {
			v = "0"
		}
		req.Header.Set(name, v)
	}
	req.Header.Set("Accept-Language", "en")
	cached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == req {
			t.Fatal("request mutated in place")
		}
		if len(r.Header) != 1 || r.Header.Get("Accept-Language") != "en" {
			t.Fatalf("unexpected forwarded headers %v", r.Header)
		}
		w.WriteHeader(204)
	})
	rec := httptest.NewRecorder()
	p.WrapHandler(cached, nil).ServeHTTP(rec, req)
	if rec.Code != 204 || req.Header.Get("X-Request-ID") == "" {
		t.Fatal("lost original request")
	}
	var disabled *Policy
	if disabled.WrapHandler(cached, nil) == nil {
		t.Fatal("nil policy lost handler")
	}
}

func TestPublicRequestRejectsUnclassifiedFraming(t *testing.T) {
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.URL.User = nil; r.Header.Set("Content-Length", "1") },
		func(r *http.Request) { r.Header.Set("Connection", "authorization") },
		func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		func(r *http.Request) { r.Trailer = http.Header{"X-Signature": []string{"x"}} },
		func(r *http.Request) { r.Method = "POST" },
	} {
		r := httptest.NewRequest("GET", "http://example/pkg", nil)
		mutate(r)
		if publicRequest(r) {
			t.Fatalf("private framing allowed: %+v", r)
		}
	}
}

func TestPolicyRejectsMalformedMappingConfiguration(t *testing.T) {
	for _, cfg := range []*Config{
		{MaxIndexMB: -1, Repositories: []Repository{{ID: "x", Bases: []string{"https://example/"}}}},
		{MaxIndexMB: 1025, Repositories: []Repository{{ID: "x", Bases: []string{"https://example/"}}}},
		{Repositories: []Repository{{ID: "", Bases: []string{"https://example/"}}}},
		{Repositories: []Repository{{ID: strings.Repeat("a", 129), Bases: []string{"https://example/"}}}},
		{Repositories: []Repository{{ID: "a\x00b", Bases: []string{"https://example/"}}}},
		{Repositories: []Repository{{ID: "x"}}},
		{Repositories: []Repository{{ID: "x", Bases: []string{"https://example/"}}, {ID: "x", Bases: []string{"https://other/"}}}},
		{Repositories: []Repository{{ID: "x", Bases: []string{"https://example/", "https://EXAMPLE/"}}}},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("accepted invalid configuration %+v", cfg)
		}
	}
	p, err := New(&Config{Repositories: []Repository{{ID: "x", Bases: []string{"https://example/a%20b"}}}})
	if err != nil || p.mappings[0].base.EscapedPath() != "/a%20b/" {
		t.Fatalf("encoded base normalization: %v %v", p, err)
	}
	if p, err := New(nil); p != nil || err != nil {
		t.Fatal("nil config should disable sharing")
	}
}

func TestPolicyRejectsUnsafeByHashPaths(t *testing.T) {
	p, err := New(&Config{Repositories: []Repository{{ID: "repo", Bases: []string{"http://example/archive/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"dists/trixie/main/../by-hash/SHA256/" + strings.Repeat("a", 64),
		"dists/trixie/main/binary-amd64/by-hash/SHA256/not-hex",
		"dists/trixie/main/binary-amd64/by-hash/SHA256/aa",
	} {
		r := httptest.NewRequest("GET", "http://example/archive/"+path, nil)
		if _, _, _, ok := p.content(r); ok {
			t.Fatalf("unsafe by-hash path accepted %s", path)
		}
	}
	r := httptest.NewRequest("GET", "http://user:password@example/archive/pkg", nil)
	if publicRequest(r) {
		t.Fatal("URL credentials classified public")
	}
	p, err = New(&Config{Repositories: []Repository{{ID: "repo", Bases: []string{"http://example/%61rchive"}}}})
	if err != nil || p.mappings[0].base.RawPath != "/%61rchive/" {
		t.Fatalf("escaped base suffix lost: %v", err)
	}
}
