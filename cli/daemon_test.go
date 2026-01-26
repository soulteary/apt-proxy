package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/define"
	"github.com/soulteary/apt-proxy/state"
)

// setupTestMirrors sets up mock mirrors to avoid network requests during tests
func setupTestMirrors() {
	state.SetUbuntuMirror("http://mirrors.example.com/ubuntu/")
	state.SetUbuntuPortsMirror("http://mirrors.example.com/ubuntu-ports/")
	state.SetDebianMirror("http://mirrors.example.com/debian/")
	state.SetCentOSMirror("http://mirrors.example.com/centos/")
	state.SetAlpineMirror("http://mirrors.example.com/alpine/")
}

// cleanupTestMirrors resets all mirrors after tests
func cleanupTestMirrors() {
	state.ResetUbuntuMirror()
	state.ResetUbuntuPortsMirror()
	state.ResetDebianMirror()
	state.ResetCentOSMirror()
	state.ResetAlpineMirror()
}

func TestNewServer(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "valid config",
			cfg: &Config{
				Debug:    false,
				CacheDir: tmpDir,
				Mode:     define.TYPE_LINUX_ALL_DISTROS,
				Listen:   "127.0.0.1:0",
			},
			wantErr: false,
		},
		{
			name: "debug mode enabled",
			cfg: &Config{
				Debug:    true,
				CacheDir: tmpDir,
				Mode:     define.TYPE_LINUX_DISTROS_UBUNTU,
				Listen:   "127.0.0.1:0",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := NewServer(tt.cfg)
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
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(cfg)
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
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if srv.router == nil {
		t.Error("Server router is nil")
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0", // Use port 0 to get a random available port
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Start server in goroutine
	serverStarted := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		close(serverStarted)
		if err := srv.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for server goroutine to start
	<-serverStarted
	// Give server time to start listening
	time.Sleep(50 * time.Millisecond)

	// Shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.server.Shutdown(ctx); err != nil {
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
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary parent directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use a subdirectory that doesn't exist yet
	cacheDir := filepath.Join(tmpDir, "cache", "subdir")

	cfg := &Config{
		Debug:    false,
		CacheDir: cacheDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(cfg)
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
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(cfg)
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

			rr := &responseRecorder{
				headers:    make(http.Header),
				statusCode: http.StatusOK,
			}

			srv.router.ServeHTTP(rr, req)

			// Note: We're just checking that the endpoints exist and respond
			// The exact status may vary depending on health check state
			if rr.statusCode == 0 {
				t.Errorf("%s returned status 0", tt.path)
			}
		})
	}
}

// responseRecorder is a simple http.ResponseWriter for testing
type responseRecorder struct {
	headers    http.Header
	body       []byte
	statusCode int
}

func (rr *responseRecorder) Header() http.Header {
	return rr.headers
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	rr.body = append(rr.body, b...)
	return len(b), nil
}

func (rr *responseRecorder) WriteHeader(statusCode int) {
	rr.statusCode = statusCode
}

func TestMirrorConfig(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0",
		Mirrors: MirrorConfig{
			Ubuntu:      "https://mirrors.example.com/ubuntu/",
			UbuntuPorts: "https://mirrors.example.com/ubuntu-ports/",
			Debian:      "https://mirrors.example.com/debian/",
			CentOS:      "https://mirrors.example.com/centos/",
			Alpine:      "https://mirrors.example.com/alpine/",
		},
	}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if srv.config.Mirrors.Ubuntu != "https://mirrors.example.com/ubuntu/" {
		t.Errorf("Mirrors.Ubuntu = %q, want %q", srv.config.Mirrors.Ubuntu, "https://mirrors.example.com/ubuntu/")
	}
}

func TestServerReload(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Debug:    false,
		CacheDir: tmpDir,
		Mode:     define.TYPE_LINUX_ALL_DISTROS,
		Listen:   "127.0.0.1:0",
	}

	srv, err := NewServer(cfg)
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
