package proxy

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
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
		{"all distros", distro.TYPE_LINUX_ALL_DISTROS},
		{"ubuntu", distro.TYPE_LINUX_DISTROS_UBUNTU},
		{"ubuntu-ports", distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS},
		{"debian", distro.TYPE_LINUX_DISTROS_DEBIAN},
		{"centos", distro.TYPE_LINUX_DISTROS_CENTOS},
		{"alpine", distro.TYPE_LINUX_DISTROS_ALPINE},
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
		wantMore bool
	}{
		{"ubuntu", distro.TYPE_LINUX_DISTROS_UBUNTU, true},
		{"debian", distro.TYPE_LINUX_DISTROS_DEBIAN, true},
		{"centos", distro.TYPE_LINUX_DISTROS_CENTOS, true},
		{"alpine", distro.TYPE_LINUX_DISTROS_ALPINE, true},
		{"all", distro.TYPE_LINUX_ALL_DISTROS, true},
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

	rewriters := CreateNewRewriters(distro.TYPE_LINUX_ALL_DISTROS)
	if rewriters == nil {
		t.Fatal("CreateNewRewriters() returned nil")
	}

	if rewriters.Ubuntu == nil {
		t.Error("Initial Ubuntu rewriter is nil")
	}

	RefreshRewriters(rewriters, distro.TYPE_LINUX_ALL_DISTROS)

	if rewriters.Ubuntu == nil {
		t.Error("Ubuntu rewriter is nil after refresh")
	}
}

func TestRefreshRewritersNil(t *testing.T) {
	RefreshRewriters(nil, distro.TYPE_LINUX_ALL_DISTROS)
}

func TestMatchingRule(t *testing.T) {
	rules := distro.UBUNTU_DEFAULT_CACHE_RULES

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

	rewriters := CreateNewRewriters(distro.TYPE_LINUX_DISTROS_UBUNTU)

	req, err := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	RewriteRequestByMode(req, rewriters, distro.TYPE_LINUX_DISTROS_UBUNTU)
}

func TestRewriteRequestByModeNilRewriters(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
	RewriteRequestByMode(req, nil, distro.TYPE_LINUX_DISTROS_UBUNTU)
}

func TestGetRewriterConfig(t *testing.T) {
	tests := []struct {
		mode     int
		wantName string
		wantNil  bool
	}{
		{distro.TYPE_LINUX_DISTROS_UBUNTU, "Ubuntu", false},
		{distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS, "Ubuntu Ports", false},
		{distro.TYPE_LINUX_DISTROS_DEBIAN, "Debian", false},
		{distro.TYPE_LINUX_DISTROS_CENTOS, "CentOS", false},
		{distro.TYPE_LINUX_DISTROS_ALPINE, "Alpine", false},
		{999, "", true},
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

	rewriters := CreateNewRewriters(distro.TYPE_LINUX_ALL_DISTROS)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			req, _ := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
			RewriteRequestByMode(req, rewriters, distro.TYPE_LINUX_DISTROS_UBUNTU)
			done <- true
		}()
	}

	go func() {
		RefreshRewriters(rewriters, distro.TYPE_LINUX_ALL_DISTROS)
		done <- true
	}()

	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestCreateRewriterWithSpecifiedMirror(t *testing.T) {
	state.SetUbuntuMirror("http://custom.mirror.com/ubuntu/")
	defer state.ResetUbuntuMirror()

	rewriter := createRewriter(distro.TYPE_LINUX_DISTROS_UBUNTU)
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

	rewriters := CreateNewRewriters(distro.TYPE_LINUX_DISTROS_UBUNTU)
	if rewriters.Ubuntu == nil {
		t.Fatal("Ubuntu rewriter is nil")
	}

	if rewriters.Ubuntu.pattern == nil {
		t.Error("Ubuntu rewriter pattern is nil")
	}

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
