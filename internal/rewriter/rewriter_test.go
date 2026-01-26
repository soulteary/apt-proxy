package rewriter

import (
	"net/http"
	"net/url"
	"testing"

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

func TestCreateNewRewriters(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	tests := []struct {
		name string
		mode int
	}{
		{"all distros", define.TYPE_LINUX_ALL_DISTROS},
		{"ubuntu", define.TYPE_LINUX_DISTROS_UBUNTU},
		{"ubuntu-ports", define.TYPE_LINUX_DISTROS_UBUNTU_PORTS},
		{"debian", define.TYPE_LINUX_DISTROS_DEBIAN},
		{"centos", define.TYPE_LINUX_DISTROS_CENTOS},
		{"alpine", define.TYPE_LINUX_DISTROS_ALPINE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriters := CreateNewRewriters(tt.mode)
			if rewriters == nil {
				t.Error("CreateNewRewriters() returned nil")
			}
		})
	}
}

func TestGetRewriteRulesByMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     int
		wantLen  int
		wantMore bool
	}{
		{"ubuntu", define.TYPE_LINUX_DISTROS_UBUNTU, 0, true},
		{"debian", define.TYPE_LINUX_DISTROS_DEBIAN, 0, true},
		{"centos", define.TYPE_LINUX_DISTROS_CENTOS, 0, true},
		{"alpine", define.TYPE_LINUX_DISTROS_ALPINE, 0, true},
		{"all", define.TYPE_LINUX_ALL_DISTROS, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := GetRewriteRulesByMode(tt.mode)
			if tt.wantMore && len(rules) == 0 {
				t.Error("GetRewriteRulesByMode() returned empty rules")
			}
		})
	}
}

func TestRefreshRewriters(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	// Create initial rewriters
	rewriters := CreateNewRewriters(define.TYPE_LINUX_ALL_DISTROS)
	if rewriters == nil {
		t.Fatal("CreateNewRewriters() returned nil")
	}

	// Verify initial state
	if rewriters.Ubuntu == nil {
		t.Error("Initial Ubuntu rewriter is nil")
	}

	// Refresh rewriters - should not panic
	RefreshRewriters(rewriters, define.TYPE_LINUX_ALL_DISTROS)

	// Verify rewriters still valid after refresh
	if rewriters.Ubuntu == nil {
		t.Error("Ubuntu rewriter is nil after refresh")
	}
}

func TestRefreshRewritersNil(t *testing.T) {
	// Should not panic when called with nil
	RefreshRewriters(nil, define.TYPE_LINUX_ALL_DISTROS)
}

func TestRefreshRewritersSingleMode(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	modes := []int{
		define.TYPE_LINUX_DISTROS_UBUNTU,
		define.TYPE_LINUX_DISTROS_DEBIAN,
		define.TYPE_LINUX_DISTROS_CENTOS,
		define.TYPE_LINUX_DISTROS_ALPINE,
	}

	for _, mode := range modes {
		t.Run("mode_"+string(rune(mode)), func(t *testing.T) {
			rewriters := CreateNewRewriters(mode)
			RefreshRewriters(rewriters, mode)
			// Should not panic
		})
	}
}

func TestMatchingRule(t *testing.T) {
	rules := define.UBUNTU_DEFAULT_CACHE_RULES

	tests := []struct {
		name      string
		path      string
		wantMatch bool
	}{
		{"ubuntu release file", "/ubuntu/dists/jammy/Release", true},
		{"ubuntu deb file", "/ubuntu/pool/main/a/apt/apt_2.4.8_amd64.deb", true},
		{"unknown path", "/unknown/path/file.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, match := MatchingRule(tt.path, rules)
			if match != tt.wantMatch {
				t.Errorf("MatchingRule() match = %v, want %v", match, tt.wantMatch)
			}
		})
	}
}

func TestRewriteRequestByMode(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	rewriters := CreateNewRewriters(define.TYPE_LINUX_DISTROS_UBUNTU)

	// Create a test request
	req, err := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Rewrite should not panic
	RewriteRequestByMode(req, rewriters, define.TYPE_LINUX_DISTROS_UBUNTU)
}

func TestRewriteRequestByModeNilRewriters(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)

	// Should not panic with nil rewriters
	RewriteRequestByMode(req, nil, define.TYPE_LINUX_DISTROS_UBUNTU)
}

func TestGetRewriterConfig(t *testing.T) {
	tests := []struct {
		mode     int
		wantName string
		wantNil  bool
	}{
		{define.TYPE_LINUX_DISTROS_UBUNTU, "Ubuntu", false},
		{define.TYPE_LINUX_DISTROS_UBUNTU_PORTS, "Ubuntu Ports", false},
		{define.TYPE_LINUX_DISTROS_DEBIAN, "Debian", false},
		{define.TYPE_LINUX_DISTROS_CENTOS, "CentOS", false},
		{define.TYPE_LINUX_DISTROS_ALPINE, "Alpine", false},
		{999, "", true}, // Unknown mode
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			getMirror, name := getRewriterConfig(tt.mode)
			if tt.wantNil {
				if getMirror != nil {
					t.Error("Expected nil getMirror function for unknown mode")
				}
			} else {
				if getMirror == nil {
					t.Error("Expected non-nil getMirror function")
				}
				if name != tt.wantName {
					t.Errorf("name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

func TestURLRewritersConcurrency(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	rewriters := CreateNewRewriters(define.TYPE_LINUX_ALL_DISTROS)

	// Test concurrent access to rewriters
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			req, _ := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
			RewriteRequestByMode(req, rewriters, define.TYPE_LINUX_DISTROS_UBUNTU)
			done <- true
		}()
	}

	// Also refresh concurrently
	go func() {
		RefreshRewriters(rewriters, define.TYPE_LINUX_ALL_DISTROS)
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestCreateRewriterWithSpecifiedMirror(t *testing.T) {
	// Set a specific mirror
	state.SetUbuntuMirror("http://custom.mirror.com/ubuntu/")
	defer state.ResetUbuntuMirror()

	rewriter := createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU)
	if rewriter == nil {
		t.Fatal("createRewriter() returned nil")
	}

	if rewriter.mirror == nil {
		t.Fatal("rewriter.mirror is nil")
	}

	expectedHost := "custom.mirror.com"
	if rewriter.mirror.Host != expectedHost {
		t.Errorf("mirror.Host = %q, want %q", rewriter.mirror.Host, expectedHost)
	}
}

func TestURLRewriterPattern(t *testing.T) {
	setupTestMirrors()
	defer cleanupTestMirrors()

	rewriters := CreateNewRewriters(define.TYPE_LINUX_DISTROS_UBUNTU)
	if rewriters.Ubuntu == nil {
		t.Fatal("Ubuntu rewriter is nil")
	}

	if rewriters.Ubuntu.pattern == nil {
		t.Error("Ubuntu rewriter pattern is nil")
	}

	// Test pattern matching
	testPaths := []struct {
		path  string
		match bool
	}{
		{"/ubuntu/dists/jammy/Release", true},
		{"/debian/dists/bookworm/Release", false},
	}

	for _, tt := range testPaths {
		testURL, _ := url.Parse("http://localhost" + tt.path)
		matched := rewriters.Ubuntu.pattern.MatchString(testURL.String())
		if matched != tt.match {
			t.Errorf("pattern.MatchString(%q) = %v, want %v", tt.path, matched, tt.match)
		}
	}
}
