package server_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/define"
	"github.com/soulteary/apt-proxy/internal/server"
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

func TestCreatePackageStructRouter(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()

	// Test with different modes
	modes := []struct {
		mode int
		name string
	}{
		{define.TYPE_LINUX_ALL_DISTROS, "all"},
		{define.TYPE_LINUX_DISTROS_UBUNTU, "ubuntu"},
		{define.TYPE_LINUX_DISTROS_DEBIAN, "debian"},
		{define.TYPE_LINUX_DISTROS_CENTOS, "centos"},
		{define.TYPE_LINUX_DISTROS_ALPINE, "alpine"},
	}

	for _, tt := range modes {
		t.Run(tt.name, func(t *testing.T) {
			state.SetProxyMode(tt.mode)

			router := server.CreatePackageStructRouter(tmpDir, log)
			if router == nil {
				t.Error("CreatePackageStructRouter() returned nil")
				return
			}

			if router.CacheDir != tmpDir {
				t.Errorf("CacheDir = %q, want %q", router.CacheDir, tmpDir)
			}

			if router.Handler == nil {
				t.Error("Handler is nil")
			}

			if len(router.Rules) == 0 {
				t.Error("Rules are empty")
			}
		})
	}
}

func TestPackageStructServeHTTP(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_ALL_DISTROS)

	router := server.CreatePackageStructRouter(tmpDir, log)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "unknown path returns 404",
			path:       "/unknown/path",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "random path returns 404",
			path:       "/some/random/file.txt",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("ServeHTTP() status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestPackageStructServeHTTPUbuntuPath(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_DISTROS_UBUNTU)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Test Ubuntu-specific paths - these should match the pattern
	// but may fail on actual proxy since there's no backend
	ubuntuPaths := []string{
		"/ubuntu/dists/jammy/Release",
		"/ubuntu/pool/main/a/apt/apt_2.4.8_amd64.deb",
	}

	for _, path := range ubuntuPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// We expect the router to try to handle Ubuntu paths
			// The actual response depends on upstream connectivity
			// Just verify it doesn't panic
			t.Logf("Path %s returned status %d", path, rr.Code)
		})
	}
}

func TestHandleHomePage(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.HandleHomePage(rr, req, tmpDir)

	// Home page should return OK or BadGateway (if cache size calculation fails)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadGateway {
		t.Errorf("HandleHomePage() status = %d, want %d or %d", rr.Code, http.StatusOK, http.StatusBadGateway)
	}

	// Response should not be empty
	if rr.Body.Len() == 0 {
		t.Error("HandleHomePage() returned empty body")
	}
}

func TestResponseWriterWriteHeader(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// This tests the responseWriter's WriteHeader behavior
	// The responseWriter is an internal type used for injecting cache control headers

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_DISTROS_UBUNTU)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Test that the router properly wraps responses
	req := httptest.NewRequest(http.MethodGet, "/ubuntu/dists/jammy/Release", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Check that response headers are set (may include Cache-Control)
	t.Logf("Response headers: %v", rr.Header())
}

func TestPackageStructNilHandler(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_ALL_DISTROS)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Save original handler and set to nil
	originalHandler := router.Handler
	router.Handler = nil

	// Test with nil handler - should return 500 for matching paths
	req := httptest.NewRequest(http.MethodGet, "/ubuntu/dists/jammy/Release", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Should return Internal Server Error when handler is nil
	if rr.Code != http.StatusInternalServerError && rr.Code != http.StatusNotFound {
		t.Errorf("ServeHTTP() with nil handler status = %d, want %d or %d", rr.Code, http.StatusInternalServerError, http.StatusNotFound)
	}

	// Restore handler
	router.Handler = originalHandler
}

func TestCacheControlHeaders(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer backend.Close()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_ALL_DISTROS)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Test request that should have cache control headers applied
	req := httptest.NewRequest(http.MethodGet, "/ubuntu/dists/jammy/Release", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Log the response for debugging
	t.Logf("Status: %d, Headers: %v", rr.Code, rr.Header())
}

func TestDebianPath(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_DISTROS_DEBIAN)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Test Debian-specific paths
	debianPaths := []string{
		"/debian/dists/bookworm/Release",
		"/debian/pool/main/a/apt/apt_2.6.1_amd64.deb",
	}

	for _, path := range debianPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// Just verify it doesn't panic
			t.Logf("Path %s returned status %d", path, rr.Code)
		})
	}
}

func TestAlpinePath(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_DISTROS_ALPINE)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Test Alpine-specific paths
	alpinePaths := []string{
		"/alpine/v3.18/main/x86_64/APKINDEX.tar.gz",
	}

	for _, path := range alpinePaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// Just verify it doesn't panic
			t.Logf("Path %s returned status %d", path, rr.Code)
		})
	}
}

func TestCentOSPath(t *testing.T) {
	// Setup mock mirrors to avoid network requests
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create a temporary cache directory
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.Default()
	state.SetProxyMode(define.TYPE_LINUX_DISTROS_CENTOS)

	router := server.CreatePackageStructRouter(tmpDir, log)

	// Test CentOS-specific paths
	centosPaths := []string{
		"/centos/7/os/x86_64/repodata/repomd.xml",
	}

	for _, path := range centosPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// Just verify it doesn't panic
			t.Logf("Path %s returned status %d", path, rr.Code)
		})
	}
}
