package httpcache_test

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/pkg/httpcache"
)

func TestCacheCleanupLRU(t *testing.T) {
	// Create a cache with a very small max size
	// Note: each item includes body + header, so actual item size is larger than body
	config := httpcache.DefaultCacheConfig().
		WithMaxSize(2000).     // 2KB limit
		WithCleanupInterval(0) // Disable automatic cleanup for deterministic testing

	cache := httpcache.NewMemoryCacheWithConfig(config)
	defer cache.Close()

	// Store items that together exceed the limit
	for i := 0; i < 10; i++ {
		body := strings.Repeat("x", 300) // ~300 bytes body + ~40 bytes header
		res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
			"Content-Length": []string{"300"},
		})
		key := "key" + string(rune('0'+i))
		if err := cache.Store(res, key); err != nil {
			t.Fatalf("failed to store item %d: %v", i, err)
		}
	}

	// Check that cache size is approximately within limit
	// Allow some tolerance for the last item being stored before eviction
	stats := cache.Stats()
	maxAllowedSize := int64(2500) // Allow some tolerance
	if stats.TotalSize > maxAllowedSize {
		t.Errorf("cache size %d exceeds expected max %d", stats.TotalSize, maxAllowedSize)
	}

	// Should not have all 10 items - some should have been evicted
	if stats.ItemCount >= 10 {
		t.Errorf("expected items to be evicted, but have all %d items", stats.ItemCount)
	}

	t.Logf("cache stats: items=%d, size=%d", stats.ItemCount, stats.TotalSize)
}

func TestCacheCleanupTTL(t *testing.T) {
	// Save original clock and restore after test
	originalClock := httpcache.Clock
	defer func() { httpcache.Clock = originalClock }()

	now := time.Now().UTC()
	httpcache.Clock = func() time.Time { return now }

	// Create a cache with short TTL
	config := httpcache.DefaultCacheConfig().
		WithTTL(1 * time.Hour).
		WithCleanupInterval(0) // Disable automatic cleanup

	cache := httpcache.NewMemoryCacheWithConfig(config)
	defer cache.Close()

	// Store an item
	body := "test body"
	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{})
	if err := cache.Store(res, "testkey"); err != nil {
		t.Fatalf("failed to store item: %v", err)
	}

	// Verify item exists
	if _, err := cache.Retrieve("testkey"); err != nil {
		t.Fatalf("item should exist: %v", err)
	}

	// Advance time past TTL
	now = now.Add(2 * time.Hour)
	httpcache.Clock = func() time.Time { return now }

	// Run cleanup
	result := cache.Cleanup()
	if result.RemovedItems != 1 {
		t.Errorf("expected 1 item removed, got %d", result.RemovedItems)
	}

	// Item should no longer exist
	if _, err := cache.Retrieve("testkey"); err != httpcache.ErrNotFoundInCache {
		t.Errorf("item should have been removed by TTL cleanup")
	}
}

func TestStaleMapCleanup(t *testing.T) {
	// Save original clock and restore after test
	originalClock := httpcache.Clock
	defer func() { httpcache.Clock = originalClock }()

	now := time.Now().UTC()
	httpcache.Clock = func() time.Time { return now }

	// Create a cache with short stale map TTL
	config := httpcache.DefaultCacheConfig().
		WithStaleMapTTL(1 * time.Hour).
		WithCleanupInterval(0) // Disable automatic cleanup

	cache := httpcache.NewMemoryCacheWithConfig(config)
	defer cache.Close()

	// Store an item and invalidate it
	body := "test body"
	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{})
	if err := cache.Store(res, "testkey"); err != nil {
		t.Fatalf("failed to store item: %v", err)
	}

	cache.Invalidate("testkey")

	// Verify stale count
	stats := cache.Stats()
	if stats.StaleCount != 1 {
		t.Errorf("expected 1 stale entry, got %d", stats.StaleCount)
	}

	// Advance time past stale map TTL
	now = now.Add(2 * time.Hour)
	httpcache.Clock = func() time.Time { return now }

	// Run cleanup
	result := cache.Cleanup()
	if result.RemovedStaleEntries != 1 {
		t.Errorf("expected 1 stale entry removed, got %d", result.RemovedStaleEntries)
	}

	// Verify stale count is now 0
	stats = cache.Stats()
	if stats.StaleCount != 0 {
		t.Errorf("expected 0 stale entries after cleanup, got %d", stats.StaleCount)
	}
}

func TestCacheStats(t *testing.T) {
	config := httpcache.DefaultCacheConfig().
		WithCleanupInterval(0)

	cache := httpcache.NewMemoryCacheWithConfig(config)
	defer cache.Close()

	// Initial stats should be empty
	stats := cache.Stats()
	if stats.ItemCount != 0 || stats.TotalSize != 0 {
		t.Errorf("expected empty stats, got items=%d size=%d", stats.ItemCount, stats.TotalSize)
	}

	// Store an item
	body := "test body"
	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{})
	if err := cache.Store(res, "testkey"); err != nil {
		t.Fatalf("failed to store item: %v", err)
	}

	// Check stats after store
	stats = cache.Stats()
	if stats.ItemCount != 1 {
		t.Errorf("expected 1 item, got %d", stats.ItemCount)
	}
	if stats.TotalSize <= 0 {
		t.Errorf("expected positive size, got %d", stats.TotalSize)
	}

	// Retrieve and check hit count
	if _, err := cache.Retrieve("testkey"); err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}
	stats = cache.Stats()
	if stats.HitCount != 1 {
		t.Errorf("expected 1 hit, got %d", stats.HitCount)
	}

	// Try to retrieve non-existent key
	if _, err := cache.Retrieve("nonexistent"); err != httpcache.ErrNotFoundInCache {
		t.Errorf("expected ErrNotFoundInCache, got %v", err)
	}
	stats = cache.Stats()
	if stats.MissCount != 1 {
		t.Errorf("expected 1 miss, got %d", stats.MissCount)
	}
}

func TestCachePurge(t *testing.T) {
	config := httpcache.DefaultCacheConfig().
		WithCleanupInterval(0)

	cache := httpcache.NewMemoryCacheWithConfig(config)
	defer cache.Close()

	// Store multiple items
	for i := 0; i < 5; i++ {
		body := "test body"
		res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{})
		key := "key" + string(rune('0'+i))
		if err := cache.Store(res, key); err != nil {
			t.Fatalf("failed to store item %d: %v", i, err)
		}
	}

	// Invalidate one item
	cache.Invalidate("key0")

	// Verify items exist
	stats := cache.Stats()
	if stats.ItemCount != 5 {
		t.Errorf("expected 5 items before purge, got %d", stats.ItemCount)
	}
	if stats.StaleCount != 1 {
		t.Errorf("expected 1 stale entry before purge, got %d", stats.StaleCount)
	}

	// Purge the cache
	if err := cache.Purge(); err != nil {
		t.Fatalf("purge failed: %v", err)
	}

	// Verify cache is empty
	stats = cache.Stats()
	if stats.ItemCount != 0 {
		t.Errorf("expected 0 items after purge, got %d", stats.ItemCount)
	}
	if stats.TotalSize != 0 {
		t.Errorf("expected 0 size after purge, got %d", stats.TotalSize)
	}
	if stats.StaleCount != 0 {
		t.Errorf("expected 0 stale entries after purge, got %d", stats.StaleCount)
	}
}

func TestCacheConfigDefaults(t *testing.T) {
	config := httpcache.DefaultCacheConfig()

	if config.MaxSize != httpcache.DefaultMaxCacheSize {
		t.Errorf("expected max size %d, got %d", httpcache.DefaultMaxCacheSize, config.MaxSize)
	}
	if config.TTL != httpcache.DefaultCacheTTL {
		t.Errorf("expected TTL %v, got %v", httpcache.DefaultCacheTTL, config.TTL)
	}
	if config.CleanupInterval != httpcache.DefaultCleanupInterval {
		t.Errorf("expected cleanup interval %v, got %v", httpcache.DefaultCleanupInterval, config.CleanupInterval)
	}
	if config.StaleMapTTL != httpcache.DefaultStaleMapTTL {
		t.Errorf("expected stale map TTL %v, got %v", httpcache.DefaultStaleMapTTL, config.StaleMapTTL)
	}
}

func TestCacheConfigValidation(t *testing.T) {
	config := &httpcache.CacheConfig{
		MaxSize:         -1,
		TTL:             -1,
		CleanupInterval: -1,
		StaleMapTTL:     -1,
	}

	config.Validate()

	if config.MaxSize != 0 {
		t.Errorf("expected max size 0 after validation, got %d", config.MaxSize)
	}
	if config.TTL != 0 {
		t.Errorf("expected TTL 0 after validation, got %v", config.TTL)
	}
	if config.CleanupInterval != 0 {
		t.Errorf("expected cleanup interval 0 after validation, got %v", config.CleanupInterval)
	}
	if config.StaleMapTTL != httpcache.DefaultStaleMapTTL {
		t.Errorf("expected default stale map TTL after validation, got %v", config.StaleMapTTL)
	}
}

func TestCacheDiskScanOnStartup(t *testing.T) {
	// Create a temporary directory for the cache
	tmpDir, err := os.MkdirTemp("", "cache-scan-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := httpcache.DefaultCacheConfig().
		WithCleanupInterval(0) // Disable automatic cleanup

	// Create first cache instance and store some items
	cache1, err := httpcache.NewDiskCacheWithConfig(tmpDir, config)
	if err != nil {
		t.Fatalf("failed to create first cache: %v", err)
	}

	// Store some items
	for i := 0; i < 3; i++ {
		body := strings.Repeat("test", 100)
		res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{})
		key := "key" + string(rune('0'+i))
		if err := cache1.Store(res, key); err != nil {
			t.Fatalf("failed to store item %d: %v", i, err)
		}
	}

	// Verify items are stored
	stats1 := cache1.Stats()
	if stats1.ItemCount != 3 {
		t.Errorf("expected 3 items in first cache, got %d", stats1.ItemCount)
	}
	originalSize := stats1.TotalSize
	t.Logf("first cache stats: items=%d, size=%d", stats1.ItemCount, stats1.TotalSize)

	// Close first cache
	cache1.Close()

	// Create second cache instance - should scan and find existing items
	cache2, err := httpcache.NewDiskCacheWithConfig(tmpDir, config)
	if err != nil {
		t.Fatalf("failed to create second cache: %v", err)
	}
	defer cache2.Close()

	// Verify items were found during scan
	stats2 := cache2.Stats()
	if stats2.ItemCount != 3 {
		t.Errorf("expected 3 items after scan, got %d", stats2.ItemCount)
	}
	if stats2.TotalSize != originalSize {
		t.Errorf("expected size %d after scan, got %d", originalSize, stats2.TotalSize)
	}
	t.Logf("second cache stats: items=%d, size=%d", stats2.ItemCount, stats2.TotalSize)
}
