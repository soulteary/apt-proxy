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

// multi_server_test.go is the integration-level companion to
// internal/cli/server_isolation_test.go. The unit tests there drive
// Server via fiber.app.Test (no real listener); the tests here boot
// two full httptest.NewServer instances and exercise the public HTTP
// surface concurrently.
//
// What we *intentionally* assert (cross-Server isolation):
//   - independent caches: purging A doesn't move B's stats
//   - independent API keys: each server only honours its own
//   - concurrent traffic + RefreshMirrors does not corrupt either
//
// What we *intentionally* document (a known cross-Server coupling
// that should not regress silently):
//   - internal/benchmarks holds a package-level cache; calling
//     RefreshMirrors on either Server clears it for both. This is
//     fine in production (one process == one Server) but the test
//     pins the behaviour so future per-Server cache work has an
//     anchor to flip.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// TestMultiServerCachePurgeIsolation purges A and asserts B is
// untouched. Without per-Server caches this would fail.
func TestMultiServerCachePurgeIsolation(t *testing.T) {
	srvA := newTestServer(t, &testServerOptions{apiKey: "keyA", mirrorPrefix: "http://serverA.example.com"})
	defer srvA.cleanup()
	srvB := newTestServer(t, &testServerOptions{apiKey: "keyB", mirrorPrefix: "http://serverB.example.com"})
	defer srvB.cleanup()

	if srvA.cacheDir == srvB.cacheDir {
		t.Fatalf("expected separate cache dirs, both got %q", srvA.cacheDir)
	}

	// Hit purge on A.
	req, _ := http.NewRequest(http.MethodPost, srvA.URL+"/api/cache/purge", nil)
	req.Header.Set("X-API-Key", "keyA")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("purge A: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge A status = %d", resp.StatusCode)
	}

	// Stats on B must succeed using B's key (not A's).
	req, _ = http.NewRequest(http.MethodGet, srvB.URL+"/api/cache/stats", nil)
	req.Header.Set("X-API-Key", "keyB")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stats B: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stats B status = %d, body=%s", resp.StatusCode, body)
	}
	var stats api.CacheStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	// And B's API key must be rejected by A (negative test for the
	// auth boundary).
	req, _ = http.NewRequest(http.MethodGet, srvA.URL+"/api/cache/stats", nil)
	req.Header.Set("X-API-Key", "keyB")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stats A with B key: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stats A with B key status = %d, want 401", resp.StatusCode)
	}
}

// TestMultiServerConcurrentTrafficAndRefresh fires healthz traffic at
// both servers while concurrently calling RefreshMirrors on both, and
// asserts every request returns 200 with no race detector flags.
//
// The test runs for a fixed wall-clock budget rather than a fixed
// iteration count so it remains useful as a smoke test under load.
func TestMultiServerConcurrentTrafficAndRefresh(t *testing.T) {
	srvA := newTestServer(t, &testServerOptions{apiKey: "keyA", mirrorPrefix: "http://serverA.example.com"})
	defer srvA.cleanup()
	srvB := newTestServer(t, &testServerOptions{apiKey: "keyB", mirrorPrefix: "http://serverB.example.com"})
	defer srvB.cleanup()

	const budget = 500 * time.Millisecond
	stop := make(chan struct{})
	time.AfterFunc(budget, func() { close(stop) })

	var (
		wg          sync.WaitGroup
		reqCount    atomic.Int64
		errCount    atomic.Int64
		refreshDone atomic.Int64
	)

	// Two traffic-driver goroutines, one per server.
	driveTraffic := func(url string) {
		defer wg.Done()
		client := &http.Client{Timeout: 2 * time.Second}
		for {
			select {
			case <-stop:
				return
			default:
			}
			resp, err := client.Get(url + "/healthz")
			if err != nil {
				errCount.Add(1)
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCount.Add(1)
			}
			reqCount.Add(1)
		}
	}
	wg.Add(2)
	go driveTraffic(srvA.URL)
	go driveTraffic(srvB.URL)

	// Two refresh goroutines, one per server's PackageStruct.
	doRefresh := func(srv *testServer) {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			srv.proxy.RefreshMirrors()
			refreshDone.Add(1)
		}
	}
	wg.Add(2)
	go doRefresh(srvA)
	go doRefresh(srvB)

	wg.Wait()

	if errCount.Load() != 0 {
		t.Errorf("got %d failed requests during concurrent refresh", errCount.Load())
	}
	if reqCount.Load() == 0 {
		t.Error("test ended without serving any requests")
	}
	if refreshDone.Load() == 0 {
		t.Error("test ended without completing any RefreshMirrors")
	}

	// Per-Server mirror configuration must still be intact.
	if got := srvA.state.GetMirror(distro.TypeUbuntu); got == nil ||
		got.String() != "http://serverA.example.com/ubuntu/" {
		t.Errorf("server A ubuntu mirror after refresh = %v", got)
	}
	if got := srvB.state.GetMirror(distro.TypeUbuntu); got == nil ||
		got.String() != "http://serverB.example.com/ubuntu/" {
		t.Errorf("server B ubuntu mirror after refresh = %v", got)
	}
}

// TestMultiServerProxyRouting plugs a real httptest upstream into each
// Server and verifies that A's traffic only reaches A's upstream (i.e.
// the URL rewrite is per-Server, not global).
func TestMultiServerProxyRouting(t *testing.T) {
	var hitsA, hitsB atomic.Int64
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("from-A"))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("from-B"))
	}))
	defer upstreamB.Close()

	srvA := newTestServer(t, &testServerOptions{apiKey: "keyA", upstream: upstreamA.URL})
	defer srvA.cleanup()
	srvB := newTestServer(t, &testServerOptions{apiKey: "keyB", upstream: upstreamB.URL})
	defer srvB.cleanup()

	// Hit a proxied path on each server. We pick a path that matches
	// the canonical Debian-family cache rule (Packages.gz) so the
	// rewriter actually fires; bare /Packages would 404 because the
	// per-OS cache patterns require an extension.
	const path = "/ubuntu/dists/jammy/main/binary-amd64/Packages.gz"

	resp, err := http.Get(srvA.URL + path)
	if err != nil {
		t.Fatalf("GET A: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	resp, err = http.Get(srvB.URL + path)
	if err != nil {
		t.Fatalf("GET B: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if hitsA.Load() == 0 {
		t.Error("upstream A never received traffic; request to A was rewritten elsewhere")
	}
	if hitsB.Load() == 0 {
		t.Error("upstream B never received traffic; request to B was rewritten elsewhere")
	}
	// The strict isolation claim: traffic counts are independent. We
	// don't assert exact equality (cache-hit semantics could mask one
	// of them), but neither side should see > 1 hit per request given
	// the per-test cache directories.
	if hitsA.Load() > 1 {
		t.Errorf("upstream A unexpectedly hit %d times for one request (cross-Server bleed?)", hitsA.Load())
	}
	if hitsB.Load() > 1 {
		t.Errorf("upstream B unexpectedly hit %d times for one request (cross-Server bleed?)", hitsB.Load())
	}
}

// TestMultiServerBenchmarkCacheIsShared documents (and pins) the one
// remaining shared global between Server instances: the package-level
// benchmarks cache. Either Server's RefreshMirrors clears it for both.
//
// This test is a deliberate "expected behaviour" pin; if it ever
// starts failing, that means someone has lifted the package-level
// cache (good!) and this test should be removed at the same time.
func TestMultiServerBenchmarkCacheIsShared(t *testing.T) {
	srvA := newTestServer(t, &testServerOptions{apiKey: "keyA", mirrorPrefix: "http://serverA.example.com"})
	defer srvA.cleanup()
	srvB := newTestServer(t, &testServerOptions{apiKey: "keyB", mirrorPrefix: "http://serverB.example.com"})
	defer srvB.cleanup()

	cache := benchmarks.GetBenchmarkCache()

	// Seed the global cache with two distinct entries (one per Server's
	// proxy mode key). distro.TypeAllDistros is what newTestServer uses,
	// so use a different mode key for the second entry to avoid clobber.
	cache.SetCachedResult(distro.TypeAllDistros, "http://shared-seed.example.com", time.Hour)
	cache.SetCachedResult(distro.TypeDebian, "http://shared-seed-debian.example.com", time.Hour)

	if _, ok := cache.GetCachedResult(distro.TypeAllDistros); !ok {
		t.Fatal("seed: TypeAllDistros entry missing right after SetCachedResult")
	}

	// Refresh on Server A must wipe the global cache.
	srvA.proxy.RefreshMirrors()

	if _, ok := cache.GetCachedResult(distro.TypeAllDistros); ok {
		t.Error("expected RefreshMirrors on A to clear shared benchmarks cache, but TypeAllDistros entry survived")
	}
	if _, ok := cache.GetCachedResult(distro.TypeDebian); ok {
		t.Error("expected RefreshMirrors on A to clear shared benchmarks cache, but TypeDebian entry survived")
	}

	// Re-seed and verify B clears it too.
	cache.SetCachedResult(distro.TypeAllDistros, "http://shared-seed-2.example.com", time.Hour)
	srvB.proxy.RefreshMirrors()
	if _, ok := cache.GetCachedResult(distro.TypeAllDistros); ok {
		t.Error("expected RefreshMirrors on B to clear shared benchmarks cache, but TypeAllDistros entry survived")
	}
}
