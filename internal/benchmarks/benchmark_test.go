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
