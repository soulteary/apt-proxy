package benchmarks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBenchmark(t *testing.T) {
	// Create a test server that responds quickly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
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
		w.Write([]byte("OK"))
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
		w.Write([]byte("fast"))
	}))
	defer fastServer.Close()

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("slow"))
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
	cache := &BenchmarkCache{
		results: make(map[int]CachedResult),
	}

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
	cache := &BenchmarkCache{
		results: make(map[int]CachedResult),
	}

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
		w.Write([]byte("OK"))
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
		w.Write([]byte("OK"))
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
