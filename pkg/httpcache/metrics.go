package httpcache

import (
	"github.com/prometheus/client_golang/prometheus"
	metrics "github.com/soulteary/metrics-kit"
)

// CacheMetrics holds Prometheus metrics for cache operations
type CacheMetrics struct {
	// CacheHits tracks the number of cache hits
	CacheHits *prometheus.CounterVec

	// CacheMisses tracks the number of cache misses
	CacheMisses *prometheus.CounterVec

	// CacheSkips tracks the number of cache skips (non-cacheable requests)
	CacheSkips prometheus.Counter

	// UpstreamDuration tracks the duration of upstream requests
	UpstreamDuration *prometheus.HistogramVec

	// CacheSizeBytes tracks the current cache size in bytes (gauge)
	CacheSizeBytes prometheus.Gauge

	// CacheItemCount tracks the current number of cached items (gauge)
	CacheItemCount prometheus.Gauge

	// CacheStaleCount tracks the current number of stale map entries (gauge)
	CacheStaleCount prometheus.Gauge

	// UpstreamErrors tracks the number of upstream errors
	UpstreamErrors *prometheus.CounterVec

	// CacheStoreOperations tracks cache store operations
	CacheStoreOperations *prometheus.CounterVec

	// CacheRetrieveOperations tracks cache retrieve operations
	CacheRetrieveOperations *prometheus.CounterVec

	// CacheEvictions tracks the number of cache evictions
	CacheEvictions *prometheus.CounterVec

	// CacheCleanupDuration tracks the duration of cleanup operations
	CacheCleanupDuration prometheus.Histogram
}

// DefaultMetrics is the default metrics instance (nil until initialized)
var DefaultMetrics *CacheMetrics

// NewCacheMetrics creates and registers cache metrics with the given registry
func NewCacheMetrics(registry *metrics.Registry) *CacheMetrics {
	cacheRegistry := registry.WithSubsystem("cache")

	m := &CacheMetrics{
		CacheHits: cacheRegistry.Counter("hits_total").
			Help("Total number of cache hits").
			Labels("method").
			BuildVec(),

		CacheMisses: cacheRegistry.Counter("misses_total").
			Help("Total number of cache misses").
			Labels("method").
			BuildVec(),

		CacheSkips: cacheRegistry.Counter("skips_total").
			Help("Total number of cache skips (non-cacheable requests)").
			Build(),

		UpstreamDuration: cacheRegistry.Histogram("upstream_request_duration_seconds").
			Help("Duration of upstream requests in seconds").
			Labels("method", "status").
			Buckets(metrics.HTTPDurationBuckets()).
			BuildVec(),

		CacheSizeBytes: cacheRegistry.Gauge("size_bytes").
			Help("Current cache size in bytes").
			Build(),

		CacheItemCount: cacheRegistry.Gauge("item_count").
			Help("Current number of cached items").
			Build(),

		CacheStaleCount: cacheRegistry.Gauge("stale_count").
			Help("Current number of stale map entries").
			Build(),

		UpstreamErrors: cacheRegistry.Counter("upstream_errors_total").
			Help("Total number of upstream errors").
			Labels("error_type").
			BuildVec(),

		CacheStoreOperations: cacheRegistry.Counter("store_operations_total").
			Help("Total number of cache store operations").
			Labels("result").
			BuildVec(),

		CacheRetrieveOperations: cacheRegistry.Counter("retrieve_operations_total").
			Help("Total number of cache retrieve operations").
			Labels("result").
			BuildVec(),

		CacheEvictions: cacheRegistry.Counter("evictions_total").
			Help("Total number of cache evictions").
			Labels("reason").
			BuildVec(),

		CacheCleanupDuration: cacheRegistry.Histogram("cleanup_duration_seconds").
			Help("Duration of cache cleanup operations in seconds").
			Buckets([]float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10}).
			Build(),
	}

	DefaultMetrics = m
	return m
}

// RecordCacheHit records a cache hit
func (m *CacheMetrics) RecordCacheHit(method string) {
	if m != nil && m.CacheHits != nil {
		m.CacheHits.WithLabelValues(method).Inc()
	}
}

// RecordCacheMiss records a cache miss
func (m *CacheMetrics) RecordCacheMiss(method string) {
	if m != nil && m.CacheMisses != nil {
		m.CacheMisses.WithLabelValues(method).Inc()
	}
}

// RecordCacheSkip records a cache skip
func (m *CacheMetrics) RecordCacheSkip() {
	if m != nil && m.CacheSkips != nil {
		m.CacheSkips.Inc()
	}
}

// RecordUpstreamDuration records the duration of an upstream request
func (m *CacheMetrics) RecordUpstreamDuration(method string, status int, durationSeconds float64) {
	if m != nil && m.UpstreamDuration != nil {
		statusStr := "success"
		if status >= 400 {
			statusStr = "error"
		}
		m.UpstreamDuration.WithLabelValues(method, statusStr).Observe(durationSeconds)
	}
}

// RecordUpstreamError records an upstream error
func (m *CacheMetrics) RecordUpstreamError(errorType string) {
	if m != nil && m.UpstreamErrors != nil {
		m.UpstreamErrors.WithLabelValues(errorType).Inc()
	}
}

// RecordStoreOperation records a cache store operation
func (m *CacheMetrics) RecordStoreOperation(success bool) {
	if m != nil && m.CacheStoreOperations != nil {
		result := "success"
		if !success {
			result = "failure"
		}
		m.CacheStoreOperations.WithLabelValues(result).Inc()
	}
}

// RecordRetrieveOperation records a cache retrieve operation
func (m *CacheMetrics) RecordRetrieveOperation(found bool) {
	if m != nil && m.CacheRetrieveOperations != nil {
		result := "hit"
		if !found {
			result = "miss"
		}
		m.CacheRetrieveOperations.WithLabelValues(result).Inc()
	}
}

// SetCacheSize sets the current cache size in bytes
func (m *CacheMetrics) SetCacheSize(sizeBytes int64) {
	if m != nil && m.CacheSizeBytes != nil {
		m.CacheSizeBytes.Set(float64(sizeBytes))
	}
}

// SetCacheItemCount sets the current number of cached items
func (m *CacheMetrics) SetCacheItemCount(count int) {
	if m != nil && m.CacheItemCount != nil {
		m.CacheItemCount.Set(float64(count))
	}
}

// SetCacheStaleCount sets the current number of stale map entries
func (m *CacheMetrics) SetCacheStaleCount(count int) {
	if m != nil && m.CacheStaleCount != nil {
		m.CacheStaleCount.Set(float64(count))
	}
}

// RecordCacheEviction records a cache eviction
func (m *CacheMetrics) RecordCacheEviction(reason string) {
	if m != nil && m.CacheEvictions != nil {
		m.CacheEvictions.WithLabelValues(reason).Inc()
	}
}

// RecordCleanupDuration records the duration of a cleanup operation
func (m *CacheMetrics) RecordCleanupDuration(durationSeconds float64) {
	if m != nil && m.CacheCleanupDuration != nil {
		m.CacheCleanupDuration.Observe(durationSeconds)
	}
}

// UpdateCacheStats updates all cache gauge metrics from stats
func (m *CacheMetrics) UpdateCacheStats(stats CacheStats) {
	if m == nil {
		return
	}
	m.SetCacheSize(stats.TotalSize)
	m.SetCacheItemCount(stats.ItemCount)
	m.SetCacheStaleCount(stats.StaleCount)
}
