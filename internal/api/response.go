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

// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/internal/system"
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

// WriteJSON writes a JSON response with proper encoding
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// WriteAppError writes a structured AppError as HTTP JSON (code + message) for API consistency.
func WriteAppError(w http.ResponseWriter, err *apperrors.AppError) {
	apperrors.WriteHTTPError(w, err)
}

// CalculateHitRate calculates the cache hit rate
func CalculateHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// FormatBytes formats bytes into a human-readable string. Uses 1024-based
// (binary) units to match filesystem conventions; delegates to
// system.ByteCountBinary so the formatting is centralised.
func FormatBytes(bytes int64) string {
	if bytes < 0 {
		return "0 B"
	}
	return system.ByteCountBinary(uint64(bytes))
}
