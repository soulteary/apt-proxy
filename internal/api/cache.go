package api

import (
	"net/http"

	logger "github.com/soulteary/logger-kit"

	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/pkg/httpcache"
)

// CacheHandler handles cache-related API endpoints
type CacheHandler struct {
	cache httpcache.ExtendedCache
	log   *logger.Logger
}

// NewCacheHandler creates a new CacheHandler
func NewCacheHandler(cache httpcache.ExtendedCache, log *logger.Logger) *CacheHandler {
	return &CacheHandler{
		cache: cache,
		log:   log,
	}
}

// HandleCacheStats returns cache statistics as JSON
func (h *CacheHandler) HandleCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAppError(w, apperrors.New(apperrors.ErrMethodNotAllowed, "Method not allowed"))
		return
	}

	stats := h.cache.Stats()

	// Update Prometheus metrics
	if httpcache.DefaultMetrics != nil {
		httpcache.DefaultMetrics.UpdateCacheStats(stats)
	}

	resp := CacheStatsResponse{
		TotalSizeBytes: stats.TotalSize,
		TotalSizeHuman: FormatBytes(stats.TotalSize),
		ItemCount:      stats.ItemCount,
		StaleCount:     stats.StaleCount,
		HitCount:       stats.HitCount,
		MissCount:      stats.MissCount,
		HitRate:        CalculateHitRate(stats.HitCount, stats.MissCount),
	}

	if err := WriteJSON(w, http.StatusOK, resp); err != nil {
		h.log.Error().Err(err).Msg("failed to write cache stats response")
	}
}

// HandleCachePurge clears all cached items
func (h *CacheHandler) HandleCachePurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAppError(w, apperrors.New(apperrors.ErrMethodNotAllowed, "Method not allowed"))
		return
	}

	// Get stats before purge
	statsBefore := h.cache.Stats()

	if err := h.cache.Purge(); err != nil {
		h.log.Error().Err(err).Msg("failed to purge cache")
		WriteAppError(w, apperrors.CacheError(apperrors.ErrCachePurge, "Failed to purge cache", err))
		return
	}

	h.log.Info().
		Int("items_removed", statsBefore.ItemCount).
		Int64("bytes_freed", statsBefore.TotalSize).
		Msg("cache purged")

	resp := CachePurgeResponse{
		Success:      true,
		ItemsRemoved: statsBefore.ItemCount,
		BytesFreed:   statsBefore.TotalSize,
	}

	if err := WriteJSON(w, http.StatusOK, resp); err != nil {
		h.log.Error().Err(err).Msg("failed to write cache purge response")
	}
}

// HandleCacheCleanup triggers a manual cleanup cycle
func (h *CacheHandler) HandleCacheCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAppError(w, apperrors.New(apperrors.ErrMethodNotAllowed, "Method not allowed"))
		return
	}

	result := h.cache.Cleanup()

	h.log.Info().
		Int("items_removed", result.RemovedItems).
		Int64("bytes_freed", result.RemovedBytes).
		Int("stale_entries_removed", result.RemovedStaleEntries).
		Dur("duration", result.Duration).
		Msg("manual cache cleanup completed")

	resp := CacheCleanupResponse{
		Success:             true,
		ItemsRemoved:        result.RemovedItems,
		BytesFreed:          result.RemovedBytes,
		StaleEntriesRemoved: result.RemovedStaleEntries,
		DurationMs:          result.Duration.Milliseconds(),
	}

	if err := WriteJSON(w, http.StatusOK, resp); err != nil {
		h.log.Error().Err(err).Msg("failed to write cache cleanup response")
	}
}
