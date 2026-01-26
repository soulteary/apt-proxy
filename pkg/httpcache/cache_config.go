package httpcache

import (
	"time"
)

// DefaultMaxCacheSize is the default maximum cache size (10 GB)
const DefaultMaxCacheSize int64 = 10 * 1024 * 1024 * 1024

// DefaultCacheTTL is the default TTL for cached items (7 days)
const DefaultCacheTTL = 7 * 24 * time.Hour

// DefaultCleanupInterval is the default interval for cache cleanup (1 hour)
const DefaultCleanupInterval = 1 * time.Hour

// DefaultStaleMapTTL is the default TTL for stale map entries (24 hours)
const DefaultStaleMapTTL = 24 * time.Hour

// CacheConfig holds configuration for cache behavior
type CacheConfig struct {
	// MaxSize is the maximum size of the cache in bytes.
	// When exceeded, the oldest items will be evicted (LRU).
	// Set to 0 to disable size-based eviction.
	MaxSize int64

	// TTL is the time-to-live for cached items.
	// Items older than this will be evicted during cleanup.
	// Set to 0 to disable TTL-based eviction.
	TTL time.Duration

	// CleanupInterval is the interval between automatic cleanup runs.
	// Set to 0 to disable automatic cleanup.
	CleanupInterval time.Duration

	// StaleMapTTL is the TTL for stale map entries.
	// Stale entries older than this will be removed.
	StaleMapTTL time.Duration
}

// DefaultCacheConfig returns a CacheConfig with sensible defaults
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		MaxSize:         DefaultMaxCacheSize,
		TTL:             DefaultCacheTTL,
		CleanupInterval: DefaultCleanupInterval,
		StaleMapTTL:     DefaultStaleMapTTL,
	}
}

// WithMaxSize sets the maximum cache size
func (c *CacheConfig) WithMaxSize(size int64) *CacheConfig {
	c.MaxSize = size
	return c
}

// WithTTL sets the TTL for cached items
func (c *CacheConfig) WithTTL(ttl time.Duration) *CacheConfig {
	c.TTL = ttl
	return c
}

// WithCleanupInterval sets the cleanup interval
func (c *CacheConfig) WithCleanupInterval(interval time.Duration) *CacheConfig {
	c.CleanupInterval = interval
	return c
}

// WithStaleMapTTL sets the stale map TTL
func (c *CacheConfig) WithStaleMapTTL(ttl time.Duration) *CacheConfig {
	c.StaleMapTTL = ttl
	return c
}

// Validate validates the configuration and applies defaults where needed
func (c *CacheConfig) Validate() *CacheConfig {
	if c.MaxSize < 0 {
		c.MaxSize = 0
	}
	if c.TTL < 0 {
		c.TTL = 0
	}
	if c.CleanupInterval < 0 {
		c.CleanupInterval = 0
	}
	if c.StaleMapTTL <= 0 {
		c.StaleMapTTL = DefaultStaleMapTTL
	}
	return c
}
