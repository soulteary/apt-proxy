package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/distro"
	"github.com/soulteary/apt-proxy/state"
)

func TestCreatePackageStructRouter(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	state.SetProxyMode(distro.TYPE_LINUX_ALL_DISTROS)

	log := logger.Default()
	ps := CreatePackageStructRouter(tmpDir, log)

	if ps == nil {
		t.Fatal("CreatePackageStructRouter() returned nil")
	}
	if ps.Handler == nil {
		t.Error("PackageStruct.Handler is nil")
	}
	if ps.CacheDir != tmpDir {
		t.Errorf("PackageStruct.CacheDir = %q, want %q", ps.CacheDir, tmpDir)
	}
}

func TestPackageStructServeHTTP(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	state.SetProxyMode(distro.TYPE_LINUX_ALL_DISTROS)

	log := logger.Default()
	ps := CreatePackageStructRouter(tmpDir, log)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"unknown path returns 404", "/unknown/path", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()
			ps.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleHomePage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	HandleHomePage(rr, req, tmpDir)

	if rr.Code != http.StatusOK {
		t.Errorf("HandleHomePage status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := rr.Body.String()
	if len(body) == 0 {
		t.Error("HandleHomePage returned empty body")
	}
}

func TestRenderInternalUrls(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apt-proxy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		url        string
		wantStatus int
	}{
		{"/", http.StatusOK},
		{"/_/ping/", http.StatusOK},
		{"/unknown", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			_, status := RenderInternalUrls(tt.url, tmpDir)
			if status != tt.wantStatus {
				t.Errorf("RenderInternalUrls(%q) status = %d, want %d", tt.url, status, tt.wantStatus)
			}
		})
	}
}

func TestGetInternalResType(t *testing.T) {
	tests := []struct {
		url      string
		wantType int
	}{
		{"/", TYPE_HOME},
		{"/_/ping/", TYPE_PING},
		{"/unknown", TYPE_NOT_FOUND},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			resType := GetInternalResType(tt.url)
			if resType != tt.wantType {
				t.Errorf("GetInternalResType(%q) = %d, want %d", tt.url, resType, tt.wantType)
			}
		})
	}
}

func TestIsInternalUrls(t *testing.T) {
	tests := []struct {
		url      string
		wantBool bool
	}{
		{"/", true},
		{"/_/ping/", true},
		{"/ubuntu/dists/jammy/Release", false},
		{"/debian/pool/main/a/apt/apt.deb", false},
		{"/centos/7/os/x86_64/", false},
		{"/alpine/v3.18/main/", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := IsInternalUrls(tt.url)
			if result != tt.wantBool {
				t.Errorf("IsInternalUrls(%q) = %v, want %v", tt.url, result, tt.wantBool)
			}
		})
	}
}

func TestGetBaseTemplate(t *testing.T) {
	tpl := GetBaseTemplate("100MB", "50", "10GB", "32MB", "10")

	if len(tpl) == 0 {
		t.Error("GetBaseTemplate returned empty string")
	}

	// Verify placeholders are replaced
	if contains(tpl, "$APT_PROXY_CACHE_SIZE") {
		t.Error("Template still contains $APT_PROXY_CACHE_SIZE placeholder")
	}
	if contains(tpl, "$APT_PROXY_FILE_NUMBER") {
		t.Error("Template still contains $APT_PROXY_FILE_NUMBER placeholder")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRefreshMirrors(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	state.SetProxyMode(distro.TYPE_LINUX_ALL_DISTROS)

	// Should not panic
	RefreshMirrors()
}
