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
//   - independent benchmark engines: a refresh on A no longer flushes B's
//     mirror selection cache (this used to be a documented coupling
//     pinned by TestMultiServerBenchmarkCacheIsShared; that test has
//     been flipped to TestMultiServerBenchmarkCacheIsIsolated below)

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

// TestMultiServerBenchmarkCacheIsIsolated asserts the per-Server
// benchmarks.Engine isolation: refreshing on Server A clears only A's
// mirror selection cache, not B's. This is the inverse of the previous
// TestMultiServerBenchmarkCacheIsShared, which pinned the (now removed)
// package-level cache as a documented coupling.
func TestMultiServerBenchmarkCacheIsIsolated(t *testing.T) {
	srvA := newTestServer(t, &testServerOptions{apiKey: "keyA", mirrorPrefix: "http://serverA.example.com"})
	defer srvA.cleanup()
	srvB := newTestServer(t, &testServerOptions{apiKey: "keyB", mirrorPrefix: "http://serverB.example.com"})
	defer srvB.cleanup()

	cacheA := srvA.proxy.BenchmarkEngine().Cache()
	cacheB := srvB.proxy.BenchmarkEngine().Cache()

	if cacheA == cacheB {
		t.Fatal("expected per-Server benchmark caches, both proxies share one instance")
	}

	// Seed both engines with distinct entries.
	cacheA.SetCachedResult(distro.TypeAllDistros, "http://seed-A.example.com", time.Hour)
	cacheB.SetCachedResult(distro.TypeAllDistros, "http://seed-B.example.com", time.Hour)
	cacheB.SetCachedResult(distro.TypeDebian, "http://seed-B-debian.example.com", time.Hour)

	if got, ok := cacheA.GetCachedResult(distro.TypeAllDistros); !ok || got != "http://seed-A.example.com" {
		t.Fatalf("seed A: GetCachedResult = (%q, %v)", got, ok)
	}
	if got, ok := cacheB.GetCachedResult(distro.TypeAllDistros); !ok || got != "http://seed-B.example.com" {
		t.Fatalf("seed B: GetCachedResult = (%q, %v)", got, ok)
	}

	// Refresh on Server A wipes A's cache.
	srvA.proxy.RefreshMirrors()

	if _, ok := cacheA.GetCachedResult(distro.TypeAllDistros); ok {
		t.Error("expected RefreshMirrors on A to clear A's benchmark cache")
	}

	// B's cache must be untouched: this is the isolation we just bought.
	if got, ok := cacheB.GetCachedResult(distro.TypeAllDistros); !ok || got != "http://seed-B.example.com" {
		t.Errorf("RefreshMirrors on A leaked into B's cache (TypeAllDistros): got=(%q, %v), want=(\"http://seed-B.example.com\", true)", got, ok)
	}
	if got, ok := cacheB.GetCachedResult(distro.TypeDebian); !ok || got != "http://seed-B-debian.example.com" {
		t.Errorf("RefreshMirrors on A leaked into B's cache (TypeDebian): got=(%q, %v)", got, ok)
	}

	// And the inverse: refreshing B leaves A's freshly-empty cache empty
	// (no entries to leak from A) but more importantly clears only B.
	cacheA.SetCachedResult(distro.TypeAllDistros, "http://reseed-A.example.com", time.Hour)
	srvB.proxy.RefreshMirrors()

	if _, ok := cacheB.GetCachedResult(distro.TypeAllDistros); ok {
		t.Error("expected RefreshMirrors on B to clear B's benchmark cache")
	}
	if got, ok := cacheA.GetCachedResult(distro.TypeAllDistros); !ok || got != "http://reseed-A.example.com" {
		t.Errorf("RefreshMirrors on B leaked into A's cache: got=(%q, %v), want=(\"http://reseed-A.example.com\", true)", got, ok)
	}

	// Sanity: neither Server's engine should be the package-level Default
	// engine. If they were, the isolation would only hold by accident.
	if srvA.proxy.BenchmarkEngine() == benchmarks.Default() {
		t.Error("Server A is using the package-level Default engine; expected a private one")
	}
	if srvB.proxy.BenchmarkEngine() == benchmarks.Default() {
		t.Error("Server B is using the package-level Default engine; expected a private one")
	}
	if srvA.proxy.BenchmarkEngine() == srvB.proxy.BenchmarkEngine() {
		t.Error("Server A and B share an engine; expected one engine per Server")
	}
}
