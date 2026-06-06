package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit"
	logger "github.com/soulteary/logger-kit"
)

// fakeCache is a lightweight in-memory ExtendedCache stub. It implements only
// the management APIs the handlers actually call (Stats / Purge / Cleanup) and
// the Cache interface methods as no-ops. Using a hand-rolled stub instead of
// httpcache.NewMemoryCacheWithConfig keeps the tests deterministic.
type fakeCache struct {
	stats        httpcache.CacheStats
	purgeErr     error
	purgeCalls   int
	cleanupCalls int
	cleanupRes   httpcache.CleanupResult
}

func (f *fakeCache) Stats() httpcache.CacheStats { return f.stats }

func (f *fakeCache) Cleanup() httpcache.CleanupResult {
	f.cleanupCalls++
	return f.cleanupRes
}

func (f *fakeCache) Purge() error {
	f.purgeCalls++
	if f.purgeErr != nil {
		return f.purgeErr
	}
	f.stats = httpcache.CacheStats{}
	return nil
}

func (f *fakeCache) Close() error                                 { return nil }
func (f *fakeCache) Header(string) (httpcache.Header, error)      { return httpcache.Header{}, nil }
func (f *fakeCache) Store(*httpcache.Resource, ...string) error   { return nil }
func (f *fakeCache) Retrieve(string) (*httpcache.Resource, error) { return nil, nil }
func (f *fakeCache) Invalidate(...string)                         {}
func (f *fakeCache) Freshen(*httpcache.Resource, ...string) error { return nil }

func newTestCacheHandler(c *fakeCache) *CacheHandler {
	return NewCacheHandler(c, logger.New(logger.Config{Format: logger.FormatJSON, Level: logger.ErrorLevel}))
}

func TestCacheHandlerStats(t *testing.T) {
	c := &fakeCache{stats: httpcache.CacheStats{
		TotalSize: 2048, ItemCount: 4, StaleCount: 1, HitCount: 30, MissCount: 10,
	}}
	h := newTestCacheHandler(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/stats", nil)
	h.HandleCacheStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got CacheStatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TotalSizeBytes != 2048 || got.ItemCount != 4 || got.HitCount != 30 || got.MissCount != 10 {
		t.Errorf("stats payload mismatch: %+v", got)
	}
	if got.HitRate < 0.74 || got.HitRate > 0.76 {
		t.Errorf("hit rate = %v, want ~0.75", got.HitRate)
	}
	if !strings.Contains(got.TotalSizeHuman, "KB") {
		t.Errorf("expected human-readable bytes, got %q", got.TotalSizeHuman)
	}
}

func TestCacheHandlerStatsRejectsNonGet(t *testing.T) {
	h := newTestCacheHandler(&fakeCache{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cache/stats", nil)
	h.HandleCacheStats(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestCacheHandlerPurge(t *testing.T) {
	c := &fakeCache{stats: httpcache.CacheStats{TotalSize: 1024, ItemCount: 7}}
	h := newTestCacheHandler(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cache/purge", nil)
	h.HandleCachePurge(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got CachePurgeResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Success || got.ItemsRemoved != 7 || got.BytesFreed != 1024 {
		t.Errorf("purge payload mismatch: %+v", got)
	}
	if c.purgeCalls != 1 {
		t.Errorf("expected one purge call, got %d", c.purgeCalls)
	}
}

func TestCacheHandlerPurgeRejectsGet(t *testing.T) {
	h := newTestCacheHandler(&fakeCache{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cache/purge", nil)
	h.HandleCachePurge(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestCacheHandlerCleanup(t *testing.T) {
	c := &fakeCache{cleanupRes: httpcache.CleanupResult{
		RemovedItems: 3, RemovedBytes: 99, RemovedStaleEntries: 2, Duration: 12 * time.Millisecond,
	}}
	h := newTestCacheHandler(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cache/cleanup", nil)
	h.HandleCacheCleanup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got CacheCleanupResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Success || got.ItemsRemoved != 3 || got.BytesFreed != 99 || got.StaleEntriesRemoved != 2 {
		t.Errorf("cleanup payload mismatch: %+v", got)
	}
	if got.DurationMs == 0 {
		t.Errorf("expected non-zero duration ms")
	}
	if c.cleanupCalls != 1 {
		t.Errorf("expected one cleanup call, got %d", c.cleanupCalls)
	}
}
