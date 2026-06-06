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

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	logger "github.com/soulteary/logger-kit"
)

func newTestLogger() *logger.Logger {
	return logger.New(logger.Config{Level: logger.ErrorLevel, Output: io.Discard})
}

func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// TestRateLimitDisabled ensures that limit=0 returns the next handler unchanged.
func TestRateLimitDisabled(t *testing.T) {
	m := NewRateLimitMiddleware(0, newTestLogger())
	wrapped := m.Wrap(okHandler())

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "1.2.3.4:9999"
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("iter %d: want 200, got %d", i, rr.Code)
		}
	}
}

// TestRateLimitEnforced ensures that requests beyond limit get 429.
func TestRateLimitEnforced(t *testing.T) {
	m := NewRateLimitMiddleware(3, newTestLogger())
	wrapped := m.Wrap(okHandler())

	statuses := make([]int, 5)
	for i := range statuses {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "9.9.9.9:1111"
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		statuses[i] = rr.Code
	}

	want := []int{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusTooManyRequests, http.StatusTooManyRequests}
	for i, s := range statuses {
		if s != want[i] {
			t.Errorf("request %d: want %d, got %d (statuses=%v)", i, want[i], s, statuses)
		}
	}
}

// TestRateLimitPerIP ensures that two distinct IPs do not share the same bucket.
func TestRateLimitPerIP(t *testing.T) {
	m := NewRateLimitMiddleware(2, newTestLogger())
	wrapped := m.Wrap(okHandler())

	hit := func(addr string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		return rr.Code
	}

	// IP A: 2 OK + 1 throttled
	if c := hit("1.1.1.1:1"); c != http.StatusOK {
		t.Fatalf("A1: %d", c)
	}
	if c := hit("1.1.1.1:2"); c != http.StatusOK {
		t.Fatalf("A2: %d", c)
	}
	if c := hit("1.1.1.1:3"); c != http.StatusTooManyRequests {
		t.Fatalf("A3: %d", c)
	}

	// IP B has its own bucket
	if c := hit("2.2.2.2:1"); c != http.StatusOK {
		t.Fatalf("B1: %d", c)
	}
}

// TestRateLimitXFFIgnoredByDefault verifies XFF is NOT honored when no trusted
// proxy is configured (default secure behaviour).
func TestRateLimitXFFIgnoredByDefault(t *testing.T) {
	m := NewRateLimitMiddleware(2, newTestLogger())
	wrapped := m.Wrap(okHandler())

	hit := func(remote, xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = remote
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		return rr.Code
	}

	// Even though XFF rotates, all requests come from the same RemoteAddr
	// and must share the bucket.
	if c := hit("9.9.9.9:1", "10.0.0.1"); c != http.StatusOK {
		t.Fatalf("1: %d", c)
	}
	if c := hit("9.9.9.9:2", "10.0.0.2"); c != http.StatusOK {
		t.Fatalf("2: %d", c)
	}
	if c := hit("9.9.9.9:3", "10.0.0.3"); c != http.StatusTooManyRequests {
		t.Fatalf("3: %d", c)
	}
}

// TestRateLimitXFFTrustedProxy verifies XFF is honored when the peer is in the
// trusted CIDR list.
func TestRateLimitXFFTrustedProxy(t *testing.T) {
	m := NewRateLimitMiddleware(1, newTestLogger(), "10.0.0.0/8")
	wrapped := m.Wrap(okHandler())

	hit := func(xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "10.1.2.3:443"
		req.Header.Set("X-Forwarded-For", xff)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		return rr.Code
	}

	// Different XFF values => independent buckets.
	if c := hit("203.0.113.1"); c != http.StatusOK {
		t.Fatalf("1: %d", c)
	}
	if c := hit("203.0.113.1"); c != http.StatusTooManyRequests {
		t.Fatalf("2: %d", c)
	}
	if c := hit("203.0.113.2"); c != http.StatusOK {
		t.Fatalf("3: %d", c)
	}
}

// TestRateLimitInvalidTrustedCIDR ensures invalid CIDR entries are skipped
// without panicking.
func TestRateLimitInvalidTrustedCIDR(t *testing.T) {
	m := NewRateLimitMiddleware(1, newTestLogger(), "not-a-cidr", "")
	if len(m.clientIP.trustedProxies) != 0 {
		t.Errorf("expected 0 trusted proxies, got %d", len(m.clientIP.trustedProxies))
	}
}

// TestRateLimitWrapFunc verifies the HandlerFunc adapter delegates through
// the Handler-shaped Wrap path: same throttling semantics, no panic on the
// HandlerFunc -> Handler bridge.
func TestRateLimitWrapFunc(t *testing.T) {
	m := NewRateLimitMiddleware(2, newTestLogger())
	calls := 0
	wrapped := m.WrapFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})

	hit := func() int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "5.5.5.5:1234"
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		return rr.Code
	}

	if c := hit(); c != http.StatusOK {
		t.Fatalf("first: %d", c)
	}
	if c := hit(); c != http.StatusOK {
		t.Fatalf("second: %d", c)
	}
	if c := hit(); c != http.StatusTooManyRequests {
		t.Fatalf("third (should throttle): %d", c)
	}
	if calls != 2 {
		t.Errorf("inner handler should run only for the 2 allowed requests, got %d", calls)
	}
}
