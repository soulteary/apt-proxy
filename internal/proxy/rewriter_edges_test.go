package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestAsyncRewriterUsesCachedMirrorWithoutElection(t *testing.T) {
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, "")
	engine := benchmarks.NewEngine()
	engine.Cache().SetCachedResult(distro.TypeUbuntu, "http://cached.example/ubuntu/", time.Hour)
	rw := createRewriterAsync(distro.TypeUbuntu, st, newTestRegistry(), &URLRewriters{}, engine)
	if rw == nil || rw.mirror.String() != "http://cached.example/ubuntu/" {
		t.Fatal("cached election ignored")
	}
	if createRewriterAsync(9999, st, newTestRegistry(), &URLRewriters{}, engine) != nil {
		t.Fatal("unknown distribution created rewriter")
	}
	p, err := NewPackageStruct(Options{State: newTestState(), Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Async: true, TransportWrapper: func(next http.RoundTripper) http.RoundTripper { return next }})
	if err != nil || p == nil {
		t.Fatalf("async wrapper construction: %v", err)
	}
}

func TestRewriterFallbacksKeepSecurityMirrorPath(t *testing.T) {
	configured := &url.URL{Scheme: "http", Host: "mirror.example", Path: "/security", RawPath: "/%invalid"}
	got := debianSecurityMirror(nil, configured, false)
	if got.Path != "/security/" || got.RawPath != "" {
		t.Fatalf("invalid raw path fallback=%v", got)
	}
	if debianSecurityMirror(nil, nil, false) != nil {
		t.Fatal("nil mirror invented target")
	}
	if hostPatternForMode(nil, distro.TypeDebian) == nil {
		t.Fatal("built-in host pattern lost")
	}
	var rewriters *URLRewriters
	if rewriters.get(1) != nil {
		t.Fatal("nil rewriters invented entry")
	}
	rewriters.set(1, nil)
	req := httptest.NewRequest("GET", "http://example/debian/dists/test/Release", nil)
	RewriteRequestByMode(req, nil, distro.TypeDebian)
	if req.URL.Host != "example" {
		t.Fatal("nil rewriters changed URL")
	}
	if len(GetRewriteRulesByMode(nil, distro.TypeUbuntu)) == 0 {
		t.Fatal("built-in rules fallback lost")
	}
}
