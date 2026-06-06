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

package mirrors

import (
	"regexp"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// TestBuildHTTPURL / TestBuildHTTPSURL / TestBuildListenAddress all
// previously had 0% coverage. They're trivial helpers, but missing
// coverage on simple things has a habit of hiding refactoring drift.
func TestBuildHTTPURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example.com/foo/", "http://example.com/foo/"},
		{"", "http://"},
	}
	for _, c := range cases {
		if got := BuildHTTPURL(c.in); got != c.want {
			t.Errorf("BuildHTTPURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildHTTPSURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example.com/foo/", "https://example.com/foo/"},
		{"", "https://"},
	}
	for _, c := range cases {
		if got := BuildHTTPSURL(c.in); got != c.want {
			t.Errorf("BuildHTTPSURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildListenAddress(t *testing.T) {
	cases := []struct {
		host, port, want string
	}{
		{"127.0.0.1", "3142", "127.0.0.1:3142"},
		{"", "3142", ":3142"},
		{"localhost", "", "localhost:"},
	}
	for _, c := range cases {
		if got := BuildListenAddress(c.host, c.port); got != c.want {
			t.Errorf("BuildListenAddress(%q,%q) = %q, want %q", c.host, c.port, got, c.want)
		}
	}
}

// TestNormalizeAliasURL pins the contract that bare host strings are
// promoted to https:// URLs while explicit http(s) inputs are passed
// through. Empty input yields empty output (i.e. "no alias resolved").
func TestNormalizeAliasURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"http://mirrors.example.com/", "http://mirrors.example.com/"},
		{"https://mirrors.example.com/", "https://mirrors.example.com/"},
		{"mirrors.example.com/foo/", "https://mirrors.example.com/foo/"},
	}
	for _, c := range cases {
		if got := normalizeAliasURL(c.in); got != c.want {
			t.Errorf("normalizeAliasURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestGetFullMirrorURL exercises the scheme branches of GetFullMirrorURL:
// explicit http(s) URLs are passed through; scheme-tagged bare hosts get
// the matching prefix; an unknown scheme falls through to https. These
// branches were previously only exercised transitively via builtin mirror
// fixtures, so a registry-shaped URLWithAlias never hit them directly.
func TestGetFullMirrorURL(t *testing.T) {
	cases := []struct {
		name string
		in   distro.URLWithAlias
		want string
	}{
		{
			name: "explicit-http-passthrough",
			in:   distro.URLWithAlias{URL: "http://mirrors.example.com/u/", Scheme: "http"},
			want: "http://mirrors.example.com/u/",
		},
		{
			name: "scheme-http-bare-host-promoted",
			in:   distro.URLWithAlias{URL: "mirrors.example.com/u/", Scheme: "http"},
			want: "http://mirrors.example.com/u/",
		},
		{
			name: "explicit-https-passthrough",
			in:   distro.URLWithAlias{URL: "https://mirrors.example.com/u/", Scheme: "https"},
			want: "https://mirrors.example.com/u/",
		},
		{
			name: "scheme-https-bare-host-promoted",
			in:   distro.URLWithAlias{URL: "mirrors.example.com/u/", Scheme: "https"},
			want: "https://mirrors.example.com/u/",
		},
		{
			name: "unknown-scheme-defaults-to-https",
			in:   distro.URLWithAlias{URL: "mirrors.example.com/u/"},
			want: "https://mirrors.example.com/u/",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetFullMirrorURL(c.in); got != c.want {
				t.Errorf("GetFullMirrorURL = %q, want %q", got, c.want)
			}
		})
	}
}

// TestMirrorsRegistryPrecedence exercises the registry-takes-precedence
// branches in GetMirrorURLByAliases / GetGeoMirrorUrlsByMode /
// GetPredefinedConfiguration. These were previously only invoked through
// the nil-registry path; with config-loaded registries shipping in
// production, the registry branches are the higher-value assertions.
func TestMirrorsRegistryPrecedence(t *testing.T) {
	reg := distro.NewBuiltinRegistry()
	const overrideURL = "https://override.example.com/debian/"
	const aliasName = "cn:custom"
	const aliasURL = "https://alias.example.com/debian/"
	// Replace the built-in Debian entry with a single-mirror override so we
	// can assert the registry path wins over the compile-time list.
	if err := reg.Unregister("debian"); err != nil {
		t.Fatalf("unregister debian: %v", err)
	}
	if err := reg.Register(&distro.RegisteredDistribution{
		ID:           "debian",
		Name:         "Debian",
		Type:         distro.TypeDebian,
		URLPattern:   regexp.MustCompile(`/debian/`),
		BenchmarkURL: "https://benchmark.example.com/debian/dists/stable/Release",
		Mirrors: []distro.URLWithAlias{
			{URL: overrideURL, Scheme: "https", Alias: "official"},
		},
		Aliases: map[string]string{aliasName: aliasURL, "raw": "https://raw.example.com/debian/"},
	}); err != nil {
		t.Fatalf("register debian: %v", err)
	}

	urls := GetGeoMirrorUrlsByMode(reg, distro.TypeDebian)
	if len(urls) == 0 || urls[0] != overrideURL {
		t.Errorf("registry mirrors should win: got %v", urls)
	}

	if got := GetMirrorURLByAliases(reg, distro.TypeDebian, aliasName); got != aliasURL {
		t.Errorf("registry alias should win: got %q", got)
	}
	// "cn:" prefix stripping: alias stored as "raw" must be reachable via
	// "cn:raw" lookup.
	if got := GetMirrorURLByAliases(reg, distro.TypeDebian, "cn:raw"); got != "https://raw.example.com/debian/" {
		t.Errorf("cn:-prefix stripping path failed: got %q", got)
	}
	if got := GetMirrorURLByAliases(reg, distro.TypeDebian, "cn:nonexistent"); got != "" {
		t.Errorf("missing alias should return empty, got %q", got)
	}

	bench, pat := GetPredefinedConfiguration(reg, distro.TypeDebian)
	if bench != "https://benchmark.example.com/debian/dists/stable/Release" {
		t.Errorf("registry benchmark URL should win: got %q", bench)
	}
	if pat == nil || !pat.MatchString("/debian/dists/stable/Release") {
		t.Errorf("registry URLPattern should match Debian path; got pat=%v", pat)
	}
}
