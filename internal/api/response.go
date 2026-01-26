// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// API response types for JSON serialization

// CacheStatsResponse holds cache statistics data
type CacheStatsResponse struct {
	TotalSizeBytes int64   `json:"total_size_bytes"`
	TotalSizeHuman string  `json:"total_size_human"`
	ItemCount      int     `json:"item_count"`
	StaleCount     int     `json:"stale_count"`
	HitCount       int64   `json:"hit_count"`
	MissCount      int64   `json:"miss_count"`
	HitRate        float64 `json:"hit_rate"`
}

// CachePurgeResponse holds the result of a cache purge operation
type CachePurgeResponse struct {
	Success      bool  `json:"success"`
	ItemsRemoved int   `json:"items_removed"`
	BytesFreed   int64 `json:"bytes_freed"`
}

// CacheCleanupResponse holds the result of a cache cleanup operation
type CacheCleanupResponse struct {
	Success             bool  `json:"success"`
	ItemsRemoved        int   `json:"items_removed"`
	BytesFreed          int64 `json:"bytes_freed"`
	StaleEntriesRemoved int   `json:"stale_entries_removed"`
	DurationMs          int64 `json:"duration_ms"`
}

// MirrorsRefreshResponse holds the result of a mirrors refresh operation
type MirrorsRefreshResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms"`
}

// ErrorResponse holds an error message for JSON responses
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// WriteJSON writes a JSON response with proper encoding
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// WriteJSONError writes a JSON error response
func WriteJSONError(w http.ResponseWriter, statusCode int, errMsg string) {
	WriteJSON(w, statusCode, ErrorResponse{Error: errMsg})
}

// CalculateHitRate calculates the cache hit rate
func CalculateHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// FormatBytes formats bytes into a human-readable string
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
