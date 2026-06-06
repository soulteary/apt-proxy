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

package proxy

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

func TestCreateNewRewriters(t *testing.T) {
	st := newTestState()
	reg := newTestRegistry()

	tests := []struct {
		name string
		mode int
	}{
		{"all distros", distro.TypeAllDistros},
		{"ubuntu", distro.TypeUbuntu},
		{"ubuntu-ports", distro.TypeUbuntuPorts},
		{"debian", distro.TypeDebian},
		{"centos", distro.TypeCentOS},
		{"alpine", distro.TypeAlpine},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriters := CreateNewRewriters(tt.mode, st, reg)
			if rewriters == nil {
				t.Error("CreateNewRewriters() returned nil")
			}
		})
	}
}

func TestGetRewriteRulesByMode(t *testing.T) {
	reg := newTestRegistry()

	tests := []struct {
		name     string
		mode     int
		wantMore bool
	}{
		{"ubuntu", distro.TypeUbuntu, true},
		{"debian", distro.TypeDebian, true},
		{"centos", distro.TypeCentOS, true},
		{"alpine", distro.TypeAlpine, true},
		{"all", distro.TypeAllDistros, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := GetRewriteRulesByMode(reg, tt.mode)
			if tt.wantMore && len(rules) == 0 {
				t.Error("GetRewriteRulesByMode() returned empty rules")
			}
		})
	}
}

func TestRefreshRewriters(t *testing.T) {
	st := newTestState()
	reg := newTestRegistry()

	rewriters := CreateNewRewriters(distro.TypeAllDistros, st, reg)
	if rewriters == nil {
		t.Fatal("CreateNewRewriters() returned nil")
	}

	if rewriters.Ubuntu == nil {
		t.Error("Initial Ubuntu rewriter is nil")
	}

	RefreshRewriters(rewriters, distro.TypeAllDistros, st, reg)

	if rewriters.Ubuntu == nil {
		t.Error("Ubuntu rewriter is nil after refresh")
	}
}

func TestRefreshRewritersNil(t *testing.T) {
	st := newTestState()
	reg := newTestRegistry()
	RefreshRewriters(nil, distro.TypeAllDistros, st, reg)
}

func TestMatchingRule(t *testing.T) {
	rules := distro.UbuntuDefaultCacheRules

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
	st := newTestState()
	reg := newTestRegistry()

	rewriters := CreateNewRewriters(distro.TypeUbuntu, st, reg)

	req, err := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	RewriteRequestByMode(req, rewriters, distro.TypeUbuntu)
}

// TestRewriteRequestByModePathPrefix ensures both Ubuntu and Debian
// rewriters preserve the mirror's path prefix and append the matched suffix.
// This guards against a regression where the Debian branch silently dropped
// the mirror path prefix.
func TestRewriteRequestByModePathPrefix(t *testing.T) {
	cases := []struct {
		name     string
		mirror   string
		mode     int
		distType int
		path     string
		wantHost string
		wantPath string
	}{
		{
			name:     "ubuntu with sub-path mirror",
			mirror:   "http://mirror.example.com/repo/ubuntu/",
			mode:     distro.TypeUbuntu,
			distType: distro.TypeUbuntu,
			path:     "/ubuntu/dists/jammy/Release",
			wantHost: "mirror.example.com",
			wantPath: "/repo/ubuntu/dists/jammy/Release",
		},
		{
			name:     "debian with sub-path mirror",
			mirror:   "http://mirror.example.com/repo/debian/",
			mode:     distro.TypeDebian,
			distType: distro.TypeDebian,
			path:     "/debian/dists/bookworm/Release",
			wantHost: "mirror.example.com",
			wantPath: "/repo/debian/dists/bookworm/Release",
		},
		{
			name:     "debian-security routed via security mirror",
			mirror:   "http://security.example.com/debian-security/",
			mode:     distro.TypeDebian,
			distType: distro.TypeDebian,
			path:     "/debian-security/dists/bookworm-security/main/binary-amd64/Release",
			wantHost: "security.example.com",
			wantPath: "/debian-security/dists/bookworm-security/main/binary-amd64/Release",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := state.NewAppState()
			reg := newTestRegistry()
			st.SetMirror(tc.distType, tc.mirror)

			rewriters := CreateNewRewriters(tc.mode, st, reg)
			req, err := http.NewRequest("GET", "http://localhost"+tc.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			RewriteRequestByMode(req, rewriters, tc.mode)

			if req.URL.Host != tc.wantHost {
				t.Errorf("Host = %q, want %q", req.URL.Host, tc.wantHost)
			}
			if req.URL.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", req.URL.Path, tc.wantPath)
			}
		})
	}
}

func TestRewriteRequestByModeNilRewriters(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
	RewriteRequestByMode(req, nil, distro.TypeUbuntu)
}

func TestGetRewriterConfig(t *testing.T) {
	tests := []struct {
		mode     int
		wantName string
		wantNil  bool
	}{
		{distro.TypeUbuntu, "Ubuntu", false},
		{distro.TypeUbuntuPorts, "Ubuntu Ports", false},
		{distro.TypeDebian, "Debian", false},
		{distro.TypeCentOS, "CentOS", false},
		{distro.TypeAlpine, "Alpine", false},
		{999, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			descriptor, name := getRewriterConfig(tt.mode)
			if tt.wantNil {
				if descriptor != nil {
					t.Error("Expected nil descriptor for unknown mode")
				}
			} else {
				if descriptor == nil {
					t.Error("Expected non-nil descriptor")
				}
				if name != tt.wantName {
					t.Errorf("name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

func TestURLRewritersConcurrency(t *testing.T) {
	st := newTestState()
	reg := newTestRegistry()

	rewriters := CreateNewRewriters(distro.TypeAllDistros, st, reg)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			req, _ := http.NewRequest("GET", "http://localhost/ubuntu/dists/jammy/Release", nil)
			RewriteRequestByMode(req, rewriters, distro.TypeUbuntu)
			done <- true
		}()
	}

	go func() {
		RefreshRewriters(rewriters, distro.TypeAllDistros, st, reg)
		done <- true
	}()

	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestCreateRewriterWithSpecifiedMirror(t *testing.T) {
	st := state.NewAppState()
	reg := newTestRegistry()
	st.SetMirror(distro.TypeUbuntu, "http://custom.mirror.com/ubuntu/")

	rewriter := createRewriter(distro.TypeUbuntu, st, reg, nil)
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
	st := newTestState()
	reg := newTestRegistry()

	rewriters := CreateNewRewriters(distro.TypeUbuntu, st, reg)
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
