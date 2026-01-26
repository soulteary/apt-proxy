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

	// UpstreamErrors tracks the number of upstream errors
	UpstreamErrors *prometheus.CounterVec

	// CacheStoreOperations tracks cache store operations
	CacheStoreOperations *prometheus.CounterVec

	// CacheRetrieveOperations tracks cache retrieve operations
	CacheRetrieveOperations *prometheus.CounterVec
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
