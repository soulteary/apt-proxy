package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	httpkit "github.com/soulteary/http-kit"
)

// TestRetryableTransportRetriesOn5xx ensures the transport retries on a 5xx
// response and eventually returns the successful response.
func TestRetryableTransportRetriesOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	rt := NewRetryableTransport(http.DefaultTransport)
	rt.SetRetryOptions(&httpkit.RetryOptions{
		MaxRetries:        3,
		RetryDelay:        1 * time.Millisecond,
		MaxRetryDelay:     5 * time.Millisecond,
		BackoffMultiplier: 1.0,
		// Treat 5xx as retryable.
		RetryableStatusCodes: []int{http.StatusBadGateway},
	})

	u, _ := url.Parse(srv.URL)
	req := &http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d (calls=%d)", resp.StatusCode, calls.Load())
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

// TestRetryableTransportNoRetryOn4xx ensures non-retryable status codes are
// returned without retrying.
func TestRetryableTransportNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	rt := NewRetryableTransport(http.DefaultTransport)
	u, _ := url.Parse(srv.URL)
	req := &http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call (no retry on 404), got %d", calls.Load())
	}
}
