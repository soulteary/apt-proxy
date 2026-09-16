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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v2"
	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// A cache entry is keyed by the rewritten upstream URL, because rewriting
// happens before the cache layer: PackageStruct.ServeHTTP points the request at
// the elected mirror and only then delegates to the cache, whose key is
// method + URL.
//
// So the mirror is part of the key, and re-electing one starts a fresh cache.
// That is the conservative reading -- two mirrors are not guaranteed to serve
// byte-identical content -- but it is surprising enough in operation ("why is
// it downloading everything again after a restart?") that it is worth pinning
// here and documenting under "Cache Keys and Mirror Selection".
func TestCacheKeyFollowsTheElectedMirror(t *testing.T) {
	newUpstream := func(body string) (string, *int64) {
		var hits int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&hits, 1)
			w.Header().Set("Cache-Control", "max-age=3600")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL, &hits
	}

	first, firstHits := newUpstream("from-mirror-1")
	second, secondHits := newUpstream("from-mirror-2")

	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, first+"/ubuntu/")
	st.SetProxyMode(distro.TypeUbuntu)

	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: newTestRegistry(),
		CacheDir: t.TempDir(),
		Logger:   logger.Default(),
		Mode:     distro.TypeUbuntu,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	// Same wiring as the daemon: the cache wraps the reverse proxy, inside
	// PackageStruct, so it sees the already-rewritten request.
	cacheDir := t.TempDir()
	cache, err := httpcache.NewDiskCacheWithConfig(cacheDir, nil)
	if err != nil {
		t.Fatalf("NewDiskCacheWithConfig: %v", err)
	}
	cached := httpcache.NewHandlerWithOptions(cache, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached

	// Stores complete asynchronously. Drain and close the way the daemon does
	// on shutdown, before t.TempDir removes the directory underneath them --
	// cleanups run last-registered-first, so this precedes the removal.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Logf("cache handler shutdown: %v", err)
		}
		if err := cache.Close(); err != nil {
			t.Logf("cache close: %v", err)
		}
	})

	const clientPath = "/ubuntu/dists/jammy/InRelease"
	get := func() string {
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://apt-proxy.example"+clientPath, nil))
		return rec.Body.String()
	}

	// The store completes asynchronously, so a second request issued straight
	// away can legitimately outrun it and miss. Wait for the entry to land
	// before asserting a hit -- otherwise this test measures scheduling.
	waitForItems := func(want int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if cache.Stats().ItemCount >= want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("cache never reached %d item(s); stats=%+v", want, cache.Stats())
	}

	get()
	waitForItems(1)
	get()
	if n := atomic.LoadInt64(firstHits); n != 1 {
		t.Fatalf("mirror 1 was contacted %d times for two identical requests, want 1 (the second must be a cache hit)", n)
	}

	// Operator switches the elected mirror: SIGHUP, /api/mirrors/refresh, a
	// restart, or a benchmark timeout.
	st.SetMirror(distro.TypeUbuntu, second+"/ubuntu/")
	ps.RefreshMirrors()

	if body := get(); body != "from-mirror-2" {
		t.Errorf("after the switch the response was %q, want the new mirror's", body)
	}
	if n := atomic.LoadInt64(secondHits); n != 1 {
		t.Errorf("mirror 2 was contacted %d times, want 1: the same client URL is a new key under the new mirror", n)
	}
}
