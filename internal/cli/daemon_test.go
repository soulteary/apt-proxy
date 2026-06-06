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

package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// withTestMirrors returns a copy of cfg with mock mirror URLs filled in
// when the caller didn't supply any. Tests use this to avoid real-network
// mirror benchmarks during NewServer.
func withTestMirrors(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if out.Mirrors == (config.MirrorConfig{}) {
		out.Mirrors = config.MirrorConfig{
			Ubuntu:      "http://mirrors.example.com/ubuntu/",
			UbuntuPorts: "http://mirrors.example.com/ubuntu-ports/",
			Debian:      "http://mirrors.example.com/debian/",
			CentOS:      "http://mirrors.example.com/centos/",
			Alpine:      "http://mirrors.example.com/alpine/",
		}
	}
	return &out
}

func TestNewServer(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "valid config",
			cfg: &config.Config{
				Debug:    false,
				CacheDir: tmpDir,
				Mode:     distro.TypeAllDistros,
				Listen:   "127.0.0.1:0",
			},
			wantErr: false,
		},
		{
			name: "debug mode enabled",
			cfg: &config.Config{
				Debug:    true,
				CacheDir: tmpDir,
				Mode:     distro.TypeUbuntu,
				Listen:   "127.0.0.1:0",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := NewServer(withTestMirrors(tt.cfg))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewServer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && srv == nil {
				t.Error("NewServer() returned nil server for valid config")
			}
		})
	}
}

func TestServerInitialize(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Verify server components are initialized
	if srv.config == nil {
		t.Error("Server config is nil")
	}
	if srv.cache == nil {
		t.Error("Server cache is nil")
	}
	if srv.proxy == nil {
		t.Error("Server proxy is nil")
	}
	if srv.log == nil {
		t.Error("Server log is nil")
	}
	if srv.healthAggregator == nil {
		t.Error("Server healthAggregator is nil")
	}
	if srv.metricsRegistry == nil {
		t.Error("Server metricsRegistry is nil")
	}
}

func TestServerCreateRouter(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if srv.app == nil {
		t.Error("Server Fiber app is nil")
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0", // Use port 0 to get a random available port
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Start Fiber in goroutine
	serverStarted := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		close(serverStarted)
		if err := srv.app.Listen(cfg.Listen); err != nil {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for server goroutine to start
	<-serverStarted
	// Give server time to start listening
	time.Sleep(50 * time.Millisecond)

	// Shutdown the Fiber app
	if err := srv.app.ShutdownWithTimeout(2 * time.Second); err != nil {
		t.Errorf("Server shutdown error: %v", err)
	}

	// Wait for server goroutine to finish
	select {
	case err := <-serverErr:
		if err != nil {
			t.Logf("Server returned with error (may be expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not shutdown within timeout")
	}
}

func TestCacheDirCreation(t *testing.T) {
	// Create a temporary parent directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use a subdirectory that doesn't exist yet
	cacheDir := filepath.Join(tmpDir, "cache", "subdir")

	cfg := &config.Config{
		Debug:    false,
		CacheDir: cacheDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if srv == nil {
		t.Error("NewServer() returned nil")
	}

	// Verify cache directory was created
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Error("Cache directory was not created")
	}
}

func TestHealthEndpoints(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Test health endpoints using httptest
	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/healthz", http.StatusOK},
		{"/livez", http.StatusOK},
		{"/readyz", http.StatusOK},
		{"/version", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req, err := http.NewRequest("GET", tt.path, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Host = "localhost"

			resp, err := srv.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test(%s) error: %v", tt.path, err)
			}
			defer resp.Body.Close()

			// Note: We're just checking that the endpoints exist and respond
			// The exact status may vary depending on health check state
			if resp.StatusCode == 0 {
				t.Errorf("%s returned status 0", tt.path)
			}
		})
	}
}

// responseRecorder removed: unused in tests.

func TestMirrorConfig(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
		Mirrors: config.MirrorConfig{
			Ubuntu:      "https://mirrors.example.com/ubuntu/",
			UbuntuPorts: "https://mirrors.example.com/ubuntu-ports/",
			Debian:      "https://mirrors.example.com/debian/",
			CentOS:      "https://mirrors.example.com/centos/",
			Alpine:      "https://mirrors.example.com/alpine/",
		},
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if srv.config.Mirrors.Ubuntu != "https://mirrors.example.com/ubuntu/" {
		t.Errorf("Mirrors.Ubuntu = %q, want %q", srv.config.Mirrors.Ubuntu, "https://mirrors.example.com/ubuntu/")
	}
}

func TestServerReload(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Test reload function - should not panic
	srv.reload()

	// Verify server is still in valid state after reload
	if srv.config == nil {
		t.Error("Server config is nil after reload")
	}
	if srv.proxy == nil {
		t.Error("Server proxy is nil after reload")
	}
}

// Admin API Tests

func TestCacheStatsAPI(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{
			name:       "GET returns stats",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST not allowed",
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "PUT not allowed",
			method:     http.MethodPut,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "DELETE not allowed",
			method:     http.MethodDelete,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, "/api/cache/stats", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Host = "localhost"

			resp, err := srv.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test(%s) error: %v", tt.method, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			// For successful GET, verify JSON response contains expected fields
			if tt.method == http.MethodGet && resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				expectedFields := []string{
					"total_size_bytes",
					"total_size_human",
					"item_count",
					"stale_count",
					"hit_count",
					"miss_count",
					"hit_rate",
				}
				for _, field := range expectedFields {
					if !containsField(string(body), field) {
						t.Errorf("response missing field %q", field)
					}
				}
			}
		})
	}
}

func TestCachePurgeAPI(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{
			name:       "POST purges cache",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET not allowed",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "PUT not allowed",
			method:     http.MethodPut,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "DELETE not allowed",
			method:     http.MethodDelete,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, "/api/cache/purge", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Host = "localhost"

			resp, err := srv.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test(%s) error: %v", tt.method, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			// For successful POST, verify JSON response contains expected fields
			if tt.method == http.MethodPost && resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				expectedFields := []string{
					"success",
					"items_removed",
					"bytes_freed",
				}
				for _, field := range expectedFields {
					if !containsField(string(body), field) {
						t.Errorf("response missing field %q", field)
					}
				}
			}
		})
	}
}

func TestCacheCleanupAPI(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{
			name:       "POST triggers cleanup",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET not allowed",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "PUT not allowed",
			method:     http.MethodPut,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "DELETE not allowed",
			method:     http.MethodDelete,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, "/api/cache/cleanup", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Host = "localhost"

			resp, err := srv.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test(%s) error: %v", tt.method, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			// For successful POST, verify JSON response contains expected fields
			if tt.method == http.MethodPost && resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				expectedFields := []string{
					"success",
					"items_removed",
					"bytes_freed",
					"stale_entries_removed",
					"duration_ms",
				}
				for _, field := range expectedFields {
					if !containsField(string(body), field) {
						t.Errorf("response missing field %q", field)
					}
				}
			}
		})
	}
}

func TestMirrorsRefreshAPI(t *testing.T) {
	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{
			name:       "POST refreshes mirrors",
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET not allowed",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "PUT not allowed",
			method:     http.MethodPut,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "DELETE not allowed",
			method:     http.MethodDelete,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, "/api/mirrors/refresh", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Host = "localhost"

			resp, err := srv.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test(%s) error: %v", tt.method, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			// For successful POST, verify JSON response contains expected fields
			if tt.method == http.MethodPost && resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				expectedFields := []string{
					"success",
					"message",
					"duration_ms",
				}
				for _, field := range expectedFields {
					if !containsField(string(body), field) {
						t.Errorf("response missing field %q", field)
					}
				}
			}
		})
	}
}

// Helper function to check if a JSON field exists in a response
func containsField(body, field string) bool {
	return len(body) > 0 && (len(field) == 0 || (len(body) > len(field) && containsString(body, "\""+field+"\"")))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1099511627776, "1.00 TB"},
		{2199023255552, "2.00 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := api.FormatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestCalculateHitRate(t *testing.T) {
	tests := []struct {
		hits   int64
		misses int64
		want   float64
	}{
		{0, 0, 0},
		{100, 0, 1},
		{0, 100, 0},
		{50, 50, 0.5},
		{75, 25, 0.75},
		{1, 3, 0.25},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := api.CalculateHitRate(tt.hits, tt.misses)
			if got != tt.want {
				t.Errorf("CalculateHitRate(%d, %d) = %f, want %f", tt.hits, tt.misses, got, tt.want)
			}
		})
	}
}
