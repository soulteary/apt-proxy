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

//go:build integration

// Package integration provides end-to-end tests for apt-proxy functionality.
package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/soulteary/apt-proxy/internal/api"
)

// TestHealthEndpoint tests the health check endpoint.
func TestHealthEndpoint(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("failed to request health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", result["status"])
	}
}

// TestCacheStatsEndpointWithoutAuth tests that cache stats requires authentication.
func TestCacheStatsEndpointWithoutAuth(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	resp, err := http.Get(ts.URL + "/api/cache/stats")
	if err != nil {
		t.Fatalf("failed to request cache stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

// TestCacheStatsEndpointWithAuth tests that cache stats works with valid API key.
func TestCacheStatsEndpointWithAuth(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/cache/stats", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-API-Key", "test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request cache stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, body)
	}

	var stats api.CacheStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Initial cache should be empty
	if stats.ItemCount != 0 {
		t.Errorf("expected 0 items, got %d", stats.ItemCount)
	}
}

// TestCachePurgeEndpoint tests the cache purge endpoint.
func TestCachePurgeEndpoint(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	// Purge requires POST method
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/cache/purge", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-API-Key", "test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request cache purge: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, body)
	}

	var result api.CachePurgeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success to be true")
	}
}

// TestCachePurgeMethodNotAllowed tests that GET requests to purge are rejected.
func TestCachePurgeMethodNotAllowed(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/cache/purge", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-API-Key", "test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request cache purge: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

// TestMirrorsRefreshEndpoint tests the mirrors refresh endpoint.
func TestMirrorsRefreshEndpoint(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/mirrors/refresh", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-API-Key", "test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request mirrors refresh: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, body)
	}

	var result api.MirrorsRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success to be true")
	}
}

// TestInvalidAPIKey tests that requests with invalid API key are rejected.
func TestInvalidAPIKey(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/cache/stats", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-API-Key", "wrong-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request cache stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

// TestBearerTokenAuth tests that Bearer token authentication works.
func TestBearerTokenAuth(t *testing.T) {
	ts := newTestServer(t, nil)
	defer ts.cleanup()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/cache/stats", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to request cache stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, resp.StatusCode, body)
	}
}
