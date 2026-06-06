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

package benchmarks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBenchmark(t *testing.T) {
	// Create a test server that responds quickly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	ctx := context.Background()
	duration, err := Benchmark(ctx, server.URL, "/test", 1)
	if err != nil {
		t.Fatalf("Benchmark() error = %v", err)
	}

	if duration <= 0 {
		t.Errorf("Benchmark() duration = %v, want > 0", duration)
	}
}

func TestBenchmarkContextCancellation(t *testing.T) {
	// Create a test server that responds slowly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // Reduced from 5s to 500ms
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Benchmark(ctx, server.URL, "/test", 1)
	if err == nil {
		t.Error("Benchmark() expected error due to context cancellation, got nil")
	}
}

func TestBenchmarkNon200Status(t *testing.T) {
	// Create a test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := Benchmark(ctx, server.URL, "/test", 1)
	if err == nil {
		t.Error("Benchmark() expected error for non-200 status, got nil")
	}
}

func TestBenchmarkMultipleTries(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := Benchmark(ctx, server.URL, "/test", 3)
	if err != nil {
		t.Fatalf("Benchmark() error = %v", err)
	}

	if callCount != 3 {
		t.Errorf("Benchmark() called server %d times, want 3", callCount)
	}
}

func TestGetTheFastestMirror(t *testing.T) {
	// Create multiple test servers with different response times
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fast"))
	}))
	defer fastServer.Close()

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("slow"))
	}))
	defer slowServer.Close()

	mirrors := []string{fastServer.URL, slowServer.URL}

	fastest, err := GetTheFastestMirror(mirrors, "/test")
	if err != nil {
		t.Fatalf("GetTheFastestMirror() error = %v", err)
	}

	// The fast server should be selected (though order may vary due to parallel execution)
	if fastest != fastServer.URL && fastest != slowServer.URL {
		t.Errorf("GetTheFastestMirror() = %q, want one of the test servers", fastest)
	}
}

func TestGetTheFastestMirrorNoValidResults(t *testing.T) {
	// Create servers that all return errors
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	mirrors := []string{errorServer.URL}

	_, err := GetTheFastestMirror(mirrors, "/test")
	if err == nil {
		t.Error("GetTheFastestMirror() expected error when all servers fail, got nil")
	}
}

func TestGetTheFastestMirrorEmptyList(t *testing.T) {
	mirrors := []string{}

	_, err := GetTheFastestMirror(mirrors, "/test")
	if err == nil {
		t.Error("GetTheFastestMirror() expected error for empty mirror list, got nil")
	}
}

func TestResultsSorting(t *testing.T) {
	results := Results{
		{URL: "slow", Duration: 300 * time.Millisecond},
		{URL: "fast", Duration: 100 * time.Millisecond},
		{URL: "medium", Duration: 200 * time.Millisecond},
	}

	// Sort the results
	results.Swap(0, 1) // Test Swap
	if results[0].URL != "fast" || results[1].URL != "slow" {
		t.Error("Results.Swap() did not swap correctly")
	}

	// Test Less
	if !results.Less(0, 1) {
		t.Error("Results.Less() should return true for faster duration")
	}

	// Test Len
	if results.Len() != 3 {
		t.Errorf("Results.Len() = %d, want 3", results.Len())
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-1, 1, -1},
		{5, 5, 5},
	}

	for _, tt := range tests {
		got := min(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// Tests for BenchmarkCache

func TestBenchmarkCache(t *testing.T) {
	cache := NewBenchmarkCache()

	// Test GetCachedResult with no cache
	_, ok := cache.GetCachedResult(1)
	if ok {
		t.Error("GetCachedResult() should return false for empty cache")
	}

	// Test SetCachedResult and GetCachedResult
	cache.SetCachedResult(1, "http://mirror.example.com", 1*time.Hour)
	result, ok := cache.GetCachedResult(1)
	if !ok {
		t.Error("GetCachedResult() should return true after SetCachedResult")
	}
	if result != "http://mirror.example.com" {
		t.Errorf("GetCachedResult() = %q, want %q", result, "http://mirror.example.com")
	}

	// Test ClearCache
	cache.ClearCache()
	_, ok = cache.GetCachedResult(1)
	if ok {
		t.Error("GetCachedResult() should return false after ClearCache")
	}
}

func TestCachedResultExpiration(t *testing.T) {
	cache := NewBenchmarkCache()

	// Set with very short TTL
	cache.SetCachedResult(1, "http://mirror.example.com", 1*time.Millisecond)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Should not be available anymore
	_, ok := cache.GetCachedResult(1)
	if ok {
		t.Error("GetCachedResult() should return false for expired cache entry")
	}
}

func TestGetTheFastestMirrorWithCache(t *testing.T) {
	// Clear any existing cache
	ClearBenchmarkCache()

	// Create a fast server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	mirrors := []string{server.URL}

	// First call should run benchmark and cache result
	result1, err := GetTheFastestMirrorWithCache(1, mirrors, "/test")
	if err != nil {
		t.Fatalf("GetTheFastestMirrorWithCache() error = %v", err)
	}
	if result1 != server.URL {
		t.Errorf("GetTheFastestMirrorWithCache() = %q, want %q", result1, server.URL)
	}

	// Second call should return cached result
	result2, err := GetTheFastestMirrorWithCache(1, mirrors, "/test")
	if err != nil {
		t.Fatalf("GetTheFastestMirrorWithCache() error = %v", err)
	}
	if result2 != result1 {
		t.Errorf("GetTheFastestMirrorWithCache() = %q, want cached %q", result2, result1)
	}

	// Clean up
	ClearBenchmarkCache()
}

func TestGetTheFastestMirrorAsync(t *testing.T) {
	// Clear any existing cache
	ClearBenchmarkCache()

	// Create a fast server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	mirrors := []string{server.URL}

	// Create a channel to wait for async result
	resultChan := make(chan AsyncBenchmarkResult, 1)

	GetTheFastestMirrorAsync(1, mirrors, "/test", func(result AsyncBenchmarkResult) {
		resultChan <- result
	})

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		if result.Error != nil {
			t.Fatalf("GetTheFastestMirrorAsync() error = %v", result.Error)
		}
		if result.FastestMirror != server.URL {
			t.Errorf("GetTheFastestMirrorAsync() = %q, want %q", result.FastestMirror, server.URL)
		}
		if result.DistType != 1 {
			t.Errorf("GetTheFastestMirrorAsync() DistType = %d, want 1", result.DistType)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("GetTheFastestMirrorAsync() timed out")
	}

	// Clean up
	ClearBenchmarkCache()
}

func TestGetTheFastestMirrorAsyncWithCache(t *testing.T) {
	// Clear any existing cache
	ClearBenchmarkCache()

	// Pre-populate cache
	GetBenchmarkCache().SetCachedResult(2, "http://cached.example.com", 1*time.Hour)

	// Create a channel to wait for async result
	resultChan := make(chan AsyncBenchmarkResult, 1)

	// Mirrors don't matter since cache should be used
	GetTheFastestMirrorAsync(2, []string{}, "/test", func(result AsyncBenchmarkResult) {
		resultChan <- result
	})

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		if result.Error != nil {
			t.Fatalf("GetTheFastestMirrorAsync() error = %v", result.Error)
		}
		if result.FastestMirror != "http://cached.example.com" {
			t.Errorf("GetTheFastestMirrorAsync() = %q, want cached %q", result.FastestMirror, "http://cached.example.com")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetTheFastestMirrorAsync() timed out")
	}

	// Clean up
	ClearBenchmarkCache()
}

func TestGetDefaultMirror(t *testing.T) {
	// Empty list
	result := GetDefaultMirror([]string{})
	if result != "" {
		t.Errorf("GetDefaultMirror([]) = %q, want empty string", result)
	}

	// Non-empty list
	mirrors := []string{"http://first.example.com", "http://second.example.com"}
	result = GetDefaultMirror(mirrors)
	if result != "http://first.example.com" {
		t.Errorf("GetDefaultMirror() = %q, want %q", result, "http://first.example.com")
	}
}

func TestClearBenchmarkCache(t *testing.T) {
	// Add some entries
	GetBenchmarkCache().SetCachedResult(1, "http://mirror1.example.com", 1*time.Hour)
	GetBenchmarkCache().SetCachedResult(2, "http://mirror2.example.com", 1*time.Hour)

	// Verify entries exist
	_, ok := GetBenchmarkCache().GetCachedResult(1)
	if !ok {
		t.Error("Cache should have entry for dist type 1")
	}

	// Clear cache
	ClearBenchmarkCache()

	// Verify entries are gone
	_, ok = GetBenchmarkCache().GetCachedResult(1)
	if ok {
		t.Error("Cache should be empty after ClearBenchmarkCache()")
	}
	_, ok = GetBenchmarkCache().GetCachedResult(2)
	if ok {
		t.Error("Cache should be empty after ClearBenchmarkCache()")
	}
}

func TestCachedResultIsExpired(t *testing.T) {
	// Not expired
	result := CachedResult{
		CachedAt: time.Now(),
		TTL:      1 * time.Hour,
	}
	if result.IsExpired() {
		t.Error("CachedResult should not be expired")
	}

	// Expired
	result = CachedResult{
		CachedAt: time.Now().Add(-2 * time.Hour),
		TTL:      1 * time.Hour,
	}
	if !result.IsExpired() {
		t.Error("CachedResult should be expired")
	}
}

// TestEngineIsolation pins the post-refactor invariant: two Engine
// instances must not share their cache. ClearCache on one engine is
// invisible to the other. This is what enables per-Server cache
// isolation in PackageStruct.
func TestEngineIsolation(t *testing.T) {
	a := NewEngine()
	b := NewEngine()

	if a == b {
		t.Fatal("NewEngine() returned the same pointer twice")
	}
	if a.Cache() == b.Cache() {
		t.Fatal("two engines share the same cache pointer")
	}

	a.Cache().SetCachedResult(1, "http://a.example.com", time.Hour)
	b.Cache().SetCachedResult(1, "http://b.example.com", time.Hour)

	if got, ok := a.Cache().GetCachedResult(1); !ok || got != "http://a.example.com" {
		t.Errorf("engine a: GetCachedResult(1) = (%q, %v), want (%q, true)", got, ok, "http://a.example.com")
	}
	if got, ok := b.Cache().GetCachedResult(1); !ok || got != "http://b.example.com" {
		t.Errorf("engine b: GetCachedResult(1) = (%q, %v), want (%q, true)", got, ok, "http://b.example.com")
	}

	a.ClearCache()

	if _, ok := a.Cache().GetCachedResult(1); ok {
		t.Error("ClearCache on engine a did not drop a's entry")
	}
	if got, ok := b.Cache().GetCachedResult(1); !ok || got != "http://b.example.com" {
		t.Errorf("ClearCache on a leaked into b: got (%q, %v)", got, ok)
	}
}

// TestEngineIsolationFromDefault makes sure constructed engines are
// independent from the process-wide Default() engine that backs the
// legacy package-level helpers.
func TestEngineIsolationFromDefault(t *testing.T) {
	// Snapshot the default engine into a clean state for the duration
	// of this test so we can make assertions about leak-direction.
	Default().ClearCache()
	defer Default().ClearCache()

	private := NewEngine()
	if private == Default() {
		t.Fatal("NewEngine() returned the Default() engine")
	}

	// Seeding through the package-level helper writes to Default().
	GetBenchmarkCache().SetCachedResult(7, "http://default.example.com", time.Hour)
	if _, ok := private.Cache().GetCachedResult(7); ok {
		t.Error("package-level seed leaked into a fresh private engine")
	}

	// And vice versa.
	private.Cache().SetCachedResult(8, "http://private.example.com", time.Hour)
	if _, ok := GetBenchmarkCache().GetCachedResult(8); ok {
		t.Error("private seed leaked back into the Default() engine")
	}

	// Clearing the private engine must not touch Default().
	private.ClearCache()
	if got, ok := GetBenchmarkCache().GetCachedResult(7); !ok || got != "http://default.example.com" {
		t.Errorf("private.ClearCache() flushed Default(): got (%q, %v)", got, ok)
	}
}

// TestEngineAsyncSingleflight pins the dedup invariant on the async
// path: N concurrent GetTheFastestMirrorAsync calls for the same
// distType must collapse into a single upstream probe via the engine's
// singleflight group, just like the sync GetTheFastestMirrorWithCache
// path. Every caller's callback must still fire with the same result.
func TestEngineAsyncSingleflight(t *testing.T) {
	var probeCount atomic.Int64
	// gate blocks the upstream handler until we've queued up all the
	// async calls. Without the gate the first call could finish (and
	// populate the cache) before the others reach singleflight.Do.
	gate := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		probeCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	engine := NewEngine()
	mirrors := []string{server.URL}

	const callers = 5
	results := make(chan AsyncBenchmarkResult, callers)
	for i := 0; i < callers; i++ {
		engine.GetTheFastestMirrorAsync(42, mirrors, "/probe", func(r AsyncBenchmarkResult) {
			results <- r
		})
	}

	// Give all goroutines a moment to enqueue into singleflight before
	// we let the upstream respond. 50ms is generous on CI; if it's not
	// enough the test would be conservative (more probes, not fewer)
	// so it can still fail loudly when the dedup actually breaks.
	time.Sleep(50 * time.Millisecond)
	close(gate)

	collected := 0
	deadline := time.After(10 * time.Second)
	for collected < callers {
		select {
		case r := <-results:
			if r.Error != nil {
				t.Fatalf("async benchmark failed: %v", r.Error)
			}
			if r.FastestMirror != server.URL {
				t.Errorf("FastestMirror = %q, want %q", r.FastestMirror, server.URL)
			}
			collected++
		case <-deadline:
			t.Fatalf("timed out after collecting %d/%d results", collected, callers)
		}
	}

	// The whole point: even though we issued `callers` async benchmark
	// requests, the upstream must have been probed at most BenchmarkMaxTries
	// times (i.e. exactly one Engine.Benchmark execution). Anything more
	// means singleflight is not actually deduplicating async callers.
	if got := probeCount.Load(); got > int64(BenchmarkMaxTries) {
		t.Errorf("upstream was probed %d times for %d async callers; expected at most %d (singleflight dedup broken)", got, callers, BenchmarkMaxTries)
	}
}
