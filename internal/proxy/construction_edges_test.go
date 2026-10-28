package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/metalink"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestProxyRejectsInvalidOptionalSubsystems(t *testing.T) {
	for _, configure := range []func(*Options){
		func(o *Options) { o.UpstreamAccess = &access.UpstreamConfig{AllowedHosts: []string{"invalid host"}} },
		func(o *Options) { o.Metalink = &metalink.Config{PublicURL: "invalid"} },
		func(o *Options) {
			o.MirrorHealth = &mirrorhealth.Config{Enabled: true, Repositories: []mirrorhealth.Repository{{ID: "unknown"}}}
		},
		func(o *Options) { o.MirrorHealth = &mirrorhealth.Config{FailureThreshold: -1} },
		func(o *Options) { o.DownloadMaxBytes = -1 },
		func(o *Options) { o.DownloadTotalBytes = -1 },
		func(o *Options) { o.DownloadTimeout = -1 },
	} {
		o := Options{State: newTestState(), Registry: newTestRegistry()}
		configure(&o)
		if _, err := NewPackageStruct(o); err == nil {
			t.Fatal("invalid subsystem configuration accepted")
		}
	}
}

func TestLateRegistryPopulationPublishesRoutingSnapshot(t *testing.T) {
	p := &PackageStruct{registry: distro.NewRegistry()}
	if p.currentRouting() == nil {
		t.Fatal("missing default routing")
	}
	p.routing.Store(&routing{})
	if len(p.currentRouting().patterns) == 0 {
		t.Fatal("empty registry lost fallback")
	}
	p.registry = distro.NewBuiltinRegistry()
	got := p.currentRouting()
	if len(got.patterns) == 0 || p.routing.Load() != got {
		t.Fatal("late registry not published")
	}
	if p.currentRouting() != got {
		t.Fatal("routing snapshot was unnecessarily replaced")
	}
	if hostPatternsFromRegistry(nil) != nil {
		t.Fatal("nil registry invented entries")
	}
	if requestHost(nil) != "" {
		t.Fatal("nil request invented host")
	}
	var absent *PackageStruct
	if absent.BenchmarkEngine() != nil || absent.RefreshMirrors() != nil {
		t.Fatal("nil proxy should be inert")
	}
	absent.MirrorHealthStatus()
}

func TestProxyUninitializedHandlerFailsClosed(t *testing.T) {
	p := &PackageStruct{log: logger.Default()}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example/ubuntu/pool/package.deb", nil))
	if rec.Code != 500 {
		t.Fatalf("uninitialized handler status=%d", rec.Code)
	}
	req := httptest.NewRequest("GET", "http://user:secret@example/pkg", nil)
	req.Header = nil
	normalizeRepositoryCredentials(req)
	if user, pw, ok := req.BasicAuth(); !ok || user != "user" || pw != "secret" || req.URL.User != nil {
		t.Fatal("URL credentials normalization failed")
	}
	p.rewriteRequest(&http.Request{}, nil, nil, "")
}

func TestDetachedRequestKeepsIndependentFraming(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example/pkg", nil)
	req.TransferEncoding = []string{"chunked"}
	req.Trailer = http.Header{"X-Checksum": {"one"}}
	detached := detachRequest(context.Background(), req)
	detached.TransferEncoding[0] = "identity"
	detached.Trailer.Set("X-Checksum", "two")
	if req.TransferEncoding[0] != "chunked" || req.Trailer.Get("X-Checksum") != "one" {
		t.Fatal("detached request aliases original")
	}
}

func TestHomeStatisticsCountsMetadataAndReusesSnapshot(t *testing.T) {
	dir := t.TempDir()
	meta := filepath.Join(dir, "header", "v1")
	if err := os.MkdirAll(meta, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "entry"), []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	s := getHomeStats(dir)
	if s.filesNumberLabel != "1" {
		t.Fatalf("metadata count=%s", s.filesNumberLabel)
	}
	if getHomeStats(dir) != s {
		t.Fatal("fresh stats not reused")
	}
	if computeHomeStats("").diskAvailable == LabelNoValidValue {
		t.Fatal("empty cache path failed working-directory disk probe")
	}
}

func TestMirrorHealthTransportWithoutScopeUsesBase(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example/pkg", nil)
	called := false
	transport := &mirrorHealthTransport{base: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r != req {
			t.Fatal("fallback request changed")
		}
		return &http.Response{StatusCode: 204, Body: http.NoBody}, nil
	})}
	res, err := transport.RoundTrip(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil || res.StatusCode != 204 || !called {
		t.Fatal("scope-free request did not use base")
	}
}

func TestPrivateRepositoryUsernameOnlyCredentials(t *testing.T) {
	// Parsing userinfo without a password must still generate Basic auth.
	req := &http.Request{URL: &url.URL{User: url.User("name")}}
	normalizeRepositoryCredentials(req)
	_, password, ok := req.BasicAuth()
	if !ok || password != "" {
		t.Fatal("username-only credentials lost")
	}
}

func TestMetalinkProxyConstructionAndRefresh(t *testing.T) {
	p, err := NewPackageStruct(Options{State: newTestState(), Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Metalink: &metalink.Config{PublicURL: "http://proxy.example:3142", AllowedMirrorHosts: []string{"mirror.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.RefreshMirrors(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://proxy.example:3142/metalink", nil))
	if rec.Code != 400 {
		t.Fatalf("missing metalink query status=%d", rec.Code)
	}
}
