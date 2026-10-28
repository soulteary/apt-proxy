package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	logger "github.com/soulteary/logger-kit/v3"
)

type disconnectedWriter struct {
	header http.Header
	status int
	writes int
}

func (w *disconnectedWriter) Header() http.Header       { return w.header }
func (w *disconnectedWriter) WriteHeader(status int)    { w.status = status }
func (w *disconnectedWriter) Write([]byte) (int, error) { w.writes++; return 0, io.ErrClosedPipe }

func TestManagementHandlersTolerateDisconnectedResponse(t *testing.T) {
	log := logger.New(logger.Config{Format: logger.FormatJSON, Level: logger.ErrorLevel})
	cache := &fakeCache{}
	h := NewCacheHandler(cache, log)
	for _, tc := range []struct {
		method  string
		handler http.HandlerFunc
	}{{"GET", h.HandleCacheStats}, {"POST", h.HandleCachePurge}, {"POST", h.HandleCacheCleanup}, {"POST", NewMirrorsHandler(log, func() error { return nil }).HandleMirrorsRefresh}} {
		w := &disconnectedWriter{header: http.Header{}}
		tc.handler(w, httptest.NewRequest(tc.method, "/api", nil))
		if w.writes != 1 || w.status != 200 {
			t.Fatalf("response not attempted: %+v", w)
		}
	}
	cache.purgeErr = errors.New("disk failure")
	rec := httptest.NewRecorder()
	h.HandleCachePurge(rec, httptest.NewRequest("POST", "/api", nil))
	if rec.Code != 500 {
		t.Fatalf("purge failure status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.HandleCacheCleanup(rec, httptest.NewRequest("GET", "/api", nil))
	if rec.Code != 405 {
		t.Fatalf("cleanup method status %d", rec.Code)
	}
}

func TestAuthFailureLoggingUsesTrustedPeer(t *testing.T) {
	log := logger.New(logger.Config{Format: logger.FormatJSON, Level: logger.WarnLevel})
	for _, extractor := range []*ClientIPExtractor{nil, NewClientIPExtractor([]string{"127.0.0.0/8"})} {
		m := NewAuthMiddleware(AuthConfig{APIKey: "secret", Logger: log, ClientIP: extractor})
		req := httptest.NewRequest("GET", "/api", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		rec := httptest.NewRecorder()
		m.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unauthenticated handler called") })).ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("status %d", rec.Code)
		}
	}
}

func TestClientIPMalformedAndUntrustedPeers(t *testing.T) {
	e := NewClientIPExtractor([]string{"127.0.0.0/8"})
	e.AddTrustedProxy(nil)
	for _, peer := range []string{"invalid", "192.0.2.1", "127.0.0.1"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = peer
		if got := e.ClientIP(req); got != peer {
			t.Fatalf("peer %q became %q", peer, got)
		}
	}
}

func TestRateLimiterPrunesOnlyExpiredClients(t *testing.T) {
	m := NewRateLimitMiddleware(2, nil)
	old := time.Now().Add(-3 * time.Minute)
	for i := 0; i < 1025; i++ {
		m.buckets[fmt.Sprint(i)] = &rateBucket{windowStart: old, count: 2}
	}
	m.buckets["recent"] = &rateBucket{windowStart: time.Now(), count: 1}
	if !m.allow("new") || len(m.buckets) != 2 {
		t.Fatalf("expired clients retained: %d", len(m.buckets))
	}
	if !m.allow("recent") || m.allow("recent") {
		t.Fatal("pruning reset a recent client limit")
	}
}
