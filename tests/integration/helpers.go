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

//go:build integration

package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/proxy"
	"github.com/soulteary/apt-proxy/internal/state"
	httpcache "github.com/soulteary/httpcache-kit"
)

// sharedTestLogger reuses one logger across newTestServer calls.
// Constructing a fresh logger per server triggers a data race against
// background goroutines from prior tests that are still flushing
// through the underlying zerolog timestamp hook (logger-kit's New
// touches process-global zerolog state). Sharing one instance keeps
// log output consistent and avoids that cross-test interference.
var (
	sharedTestLoggerOnce sync.Once
	sharedTestLogger     *logger.Logger
)

func getSharedTestLogger() *logger.Logger {
	sharedTestLoggerOnce.Do(func() {
		sharedTestLogger = logger.New(logger.Config{
			Level:  logger.ErrorLevel,
			Output: io.Discard,
		})
	})
	return sharedTestLogger
}

// testServer wraps an httptest.Server with the per-test cache + proxy
// scaffolding. Several integration tests share this shape; co-locating it
// here avoids the previous copy in proxy_test.go and keeps every new
// integration test on the same canonical shape.
type testServer struct {
	*httptest.Server
	cacheDir string
	cache    httpcache.ExtendedCache
	proxy    *proxy.PackageStruct
	state    *state.AppState
	apiKey   string
}

// testServerOptions tweaks a single testServer instance. Defaults are
// chosen so the existing tests don't need to set anything.
type testServerOptions struct {
	// apiKey overrides the API key the auth middleware enforces. Defaults
	// to "test-api-key" so historical tests keep passing.
	apiKey string
	// mirrorPrefix is prepended to the mock mirror URLs. Multi-server
	// isolation tests use it so each server points at distinguishable
	// upstreams (e.g. "http://serverA.example.com").
	mirrorPrefix string
	// upstream lets the caller plug a real httptest upstream as the
	// mirror target; when set, mirrorPrefix is ignored. Used by tests
	// that want to assert on the actual proxied request.
	upstream string
}

// newTestServer creates a new test server with a temporary cache directory.
// Pass nil opts for the historical defaults.
func newTestServer(t *testing.T, opts *testServerOptions) *testServer {
	t.Helper()

	if opts == nil {
		opts = &testServerOptions{}
	}
	apiKey := opts.apiKey
	if apiKey == "" {
		apiKey = "test-api-key"
	}
	prefix := opts.mirrorPrefix
	if prefix == "" {
		prefix = "http://mirrors.example.com"
	}
	if opts.upstream != "" {
		prefix = opts.upstream
	}

	cacheDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp cache dir: %v", err)
	}

	st := state.NewAppState()
	st.SetProxyMode(distro.TypeAllDistros)
	st.SetMirror(distro.TypeUbuntu, prefix+"/ubuntu/")
	st.SetMirror(distro.TypeUbuntuPorts, prefix+"/ubuntu-ports/")
	st.SetMirror(distro.TypeDebian, prefix+"/debian/")
	st.SetMirror(distro.TypeCentOS, prefix+"/centos/")
	st.SetMirror(distro.TypeAlpine, prefix+"/alpine/")
	reg := distro.NewBuiltinRegistry()

	cache, err := httpcache.NewDiskCacheWithConfig(cacheDir, httpcache.DefaultCacheConfig())
	if err != nil {
		os.RemoveAll(cacheDir)
		t.Fatalf("failed to create cache: %v", err)
	}

	log := getSharedTestLogger()

	proxyRouter, err := proxy.NewPackageStruct(proxy.Options{
		State:    st,
		Registry: reg,
		CacheDir: cacheDir,
		Logger:   log,
		Mode:     distro.TypeAllDistros,
		Async:    true,
	})
	if err != nil {
		os.RemoveAll(cacheDir)
		t.Fatalf("failed to create proxy router: %v", err)
	}

	cachedHandler := httpcache.NewHandler(cache, proxyRouter.Handler)
	proxyRouter.Handler = cachedHandler

	cacheHandler := api.NewCacheHandler(cache, log)
	mirrorsHandler := api.NewMirrorsHandler(log, proxyRouter.RefreshMirrors)
	authMiddleware := api.NewAuthMiddleware(api.AuthConfig{
		APIKey: apiKey,
		Logger: log,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/cache/stats", authMiddleware.WrapFunc(cacheHandler.HandleCacheStats))
	mux.HandleFunc("/api/cache/purge", authMiddleware.WrapFunc(cacheHandler.HandleCachePurge))
	mux.HandleFunc("/api/cache/cleanup", authMiddleware.WrapFunc(cacheHandler.HandleCacheCleanup))
	mux.HandleFunc("/api/mirrors/refresh", authMiddleware.WrapFunc(mirrorsHandler.HandleMirrorsRefresh))
	// Catch-all for proxied requests so multi-server tests can fire real
	// HTTP traffic at the mirror upstream. We route through the
	// PackageStruct's own ServeHTTP (not the bare cached handler) so the
	// URL-rewrite step actually runs before the reverse-proxy fires.
	mux.Handle("/", proxyRouter)

	server := httptest.NewServer(mux)

	return &testServer{
		Server:   server,
		cacheDir: cacheDir,
		cache:    cache,
		proxy:    proxyRouter,
		state:    st,
		apiKey:   apiKey,
	}
}

// cleanup releases the test server, the cache, and the on-disk cache
// directory. Safe to call from a deferred statement.
func (ts *testServer) cleanup() {
	ts.Close()
	if ts.cache != nil {
		_ = ts.cache.Close()
	}
	_ = os.RemoveAll(ts.cacheDir)
}
