package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestRepositoryCachePolicyEndToEnd(t *testing.T) {
	for _, policy := range []string{"", "no-store", "private", "no-cache", "max-age=0"} {
		t.Run(policy, func(t *testing.T) {
			var hits atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if policy != "" {
					w.Header().Set("Cache-Control", policy)
				}
				_, _ = w.Write([]byte("repository metadata"))
			}))
			defer upstream.Close()
			st := newTestState()
			st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
			st.SetProxyMode(distro.TypeUbuntu)
			ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default()})
			if err != nil {
				t.Fatal(err)
			}
			cache, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			cached := httpcache.NewHandlerWithOptions(cache, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
			cached.Shared = true
			ps.Handler = cached
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := cached.Shutdown(ctx); err != nil {
					t.Error(err)
				}
				if err := cache.Close(); err != nil {
					t.Error(err)
				}
			})
			get := func(directive string, path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, "http://proxy.example/ubuntu/"+path, nil)
				if directive != "" {
					req.Header.Set("Cache-Control", directive)
				}
				rec := httptest.NewRecorder()
				ps.ServeHTTP(rec, req)
				return rec
			}
			const path = "dists/jammy/InRelease"
			rec := get("", path)
			want := policy
			if want == "" {
				want = "max-age=3600"
			}
			if got := rec.Header().Get("Cache-Control"); got != want {
				t.Fatalf("policy = %q, want %q", got, want)
			}
			// A cache-only read cannot race into an upstream download. Poll only for
			// policies eligible for storage; the cache completes writes asynchronously.
			if policy == "" {
				deadline := time.Now().Add(2 * time.Second)
				for cache.Stats().ItemCount == 0 && time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
				}
				if cache.Stats().ItemCount == 0 {
					t.Fatal("default policy did not reach cache storage")
				}
				get("", path)
				if hits.Load() != 1 {
					t.Fatal("default freshness did not produce a cache hit")
				}
				get("no-cache", path)
				if hits.Load() != 2 {
					t.Fatal("client no-cache did not refresh upstream")
				}
			} else {
				get("", path)
				if hits.Load() != 2 {
					t.Fatalf("restrictive policy served stale/shared response: hits=%d", hits.Load())
				}
			}
			before := hits.Load()
			rec = get("only-if-cached", "dists/missing/InRelease")
			if rec.Code != http.StatusGatewayTimeout || hits.Load() != before {
				t.Fatalf("cache-only miss: status=%d upstream hits=%d", rec.Code, hits.Load())
			}
		})
	}
}

func TestRepositoryCachePolicyPreservesUpstreamHeaders(t *testing.T) {
	rule := &distro.Rule{CacheControl: "max-age=3600"}
	for _, status := range []int{200, 404, 500} {
		h := http.Header{"Cache-Control": []string{"public, max-age=10", "no-store"}}
		applyRepositoryCachePolicy(h, status, rule)
		if got := h.Values("Cache-Control"); len(got) != 2 || got[0] != "public, max-age=10" || got[1] != "no-store" {
			t.Fatalf("upstream policy changed: %v", got)
		}
	}
	h := make(http.Header)
	applyRepositoryCachePolicy(h, 404, rule)
	if h.Get("Cache-Control") != "public, max-age=30" {
		t.Fatal("missing short negative-cache policy")
	}
}
