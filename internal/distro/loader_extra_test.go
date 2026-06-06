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

package distro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDistributionName covers every known type plus the unknown-type
// fallback. Previously 0% covered.
func TestDistributionName(t *testing.T) {
	cases := []struct {
		t    int
		want string
	}{
		{TypeUbuntu, DistroUbuntu},
		{TypeUbuntuPorts, DistroUbuntuPorts},
		{TypeDebian, DistroDebian},
		{TypeCentOS, DistroCentOS},
		{TypeAlpine, DistroAlpine},
		{TypeAllDistros, ""},
		{9999, ""},
	}
	for _, c := range cases {
		if got := DistributionName(c.t); got != c.want {
			t.Errorf("DistributionName(%d) = %q, want %q", c.t, got, c.want)
		}
	}
}

// writeTempYAML writes content to a temp file and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "distributions.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return path
}

// TestLoaderLoadValid covers the happy path for Loader.Load: a valid
// YAML returns a populated DistributionsConfig and primes GetConfig.
func TestLoaderLoadValid(t *testing.T) {
	yaml := `distributions:
  - id: foo
    name: Foo
    type: 7
    url_pattern: "/foo/(.+)$"
    benchmark_url: "/foo/test"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100"
        rewrite: true
    mirrors:
      official:
        - "https://foo.example.com/"
    aliases:
      tuna: "https://mirrors.tuna.tsinghua.edu.cn/foo/"
`
	path := writeTempYAML(t, yaml)
	loader := NewLoader(path)

	got, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || len(got.Distributions) != 1 {
		t.Fatalf("expected 1 distribution, got %#v", got)
	}
	if loader.GetConfig() == nil {
		t.Error("GetConfig() returned nil after Load")
	}

	// Reload should produce the same config (idempotent).
	got2, err := loader.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got2 == nil || len(got2.Distributions) != 1 {
		t.Fatalf("Reload returned %#v", got2)
	}
}

// TestLoaderLoadMissingFile asserts that a non-existent path bubbles
// up an error rather than crashing or silently returning nil.
func TestLoaderLoadMissingFile(t *testing.T) {
	loader := NewLoader("/this/path/should/not/exist.yaml")
	if _, err := loader.Load(); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestLoaderLoadNoPath returns nil/nil to signal "use built-in
// defaults" when no path is given and the default search paths
// contain nothing.
func TestLoaderLoadNoPath(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Also mask HOME so the $HOME-based default path can't accidentally
	// match a developer's local distributions.yaml.
	t.Setenv("HOME", tmp)

	loader := NewLoader("")
	got, err := loader.Load()
	if err != nil {
		t.Fatalf("Load with no path: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil config when no file is found, got %#v", got)
	}
}

// TestLoaderValidateDistribution covers each individual error branch
// of validateDistribution by feeding minimal-but-broken YAML through
// Load (which calls validateDistribution per entry).
func TestLoaderValidateDistribution(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			"missing id",
			`distributions:
  - name: Foo
    type: 7
    url_pattern: "/foo/(.+)$"
    benchmark_url: "/foo/test"
`,
			"distribution ID is required",
		},
		{
			"missing name",
			`distributions:
  - id: foo
    type: 7
    url_pattern: "/foo/(.+)$"
    benchmark_url: "/foo/test"
`,
			"distribution name is required",
		},
		{
			"missing url_pattern",
			`distributions:
  - id: foo
    name: Foo
    type: 7
    benchmark_url: "/foo/test"
`,
			"URL pattern is required",
		},
		{
			"missing benchmark_url",
			`distributions:
  - id: foo
    name: Foo
    type: 7
    url_pattern: "/foo/(.+)$"
`,
			"benchmark URL is required",
		},
		{
			"invalid url_pattern regex",
			`distributions:
  - id: foo
    name: Foo
    type: 7
    url_pattern: "/foo/[(.+"
    benchmark_url: "/foo/test"
`,
			"invalid URL pattern regex",
		},
		{
			"invalid cache rule pattern",
			`distributions:
  - id: foo
    name: Foo
    type: 7
    url_pattern: "/foo/(.+)$"
    benchmark_url: "/foo/test"
    cache_rules:
      - pattern: "[invalid"
        cache_control: "max-age=100"
`,
			"invalid pattern regex",
		},
		{
			"empty cache rule pattern",
			`distributions:
  - id: foo
    name: Foo
    type: 7
    url_pattern: "/foo/(.+)$"
    benchmark_url: "/foo/test"
    cache_rules:
      - pattern: ""
        cache_control: "max-age=100"
`,
			"pattern is required",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTempYAML(t, c.yaml)
			loader := NewLoader(path)
			_, err := loader.Load()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantSub)
			}
		})
	}
}

// TestRegistryReloadAndLoadFromConfig covers the full Reload pipeline
// on a Registry: built-ins are restored, then the YAML adds a new
// distribution that becomes visible through the public accessors.
func TestRegistryReloadAndLoadFromConfig(t *testing.T) {
	yaml := `distributions:
  - id: customdistro
    name: Custom
    type: 42
    url_pattern: "/customdistro/(.+)$"
    benchmark_url: "/customdistro/test"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100"
        rewrite: true
    mirrors:
      official:
        - "https://custom.example.com/"
    aliases:
      mirror: "https://custom-alias.example.com/"
`
	path := writeTempYAML(t, yaml)

	reg := NewBuiltinRegistry()
	if err := reg.Reload(path); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	d, ok := reg.GetByType(42)
	if !ok {
		t.Fatal("custom distribution not registered after Reload")
	}
	if d.ID != "customdistro" || d.URLPattern == nil {
		t.Errorf("registered distribution = %#v", d)
	}

	// Built-ins must still be present.
	if _, ok := reg.GetByType(TypeUbuntu); !ok {
		t.Error("built-in Ubuntu missing after Reload (built-ins were not re-applied)")
	}
}

// TestRegistryReloadInvalidYAML asserts that a parse error preserves
// the prior registry state (we don't lose the built-ins on bad input).
func TestRegistryReloadInvalidYAML(t *testing.T) {
	path := writeTempYAML(t, "this: is: not: valid: yaml: [")

	reg := NewBuiltinRegistry()
	if err := reg.Reload(path); err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if _, ok := reg.GetByType(TypeUbuntu); !ok {
		t.Error("built-in Ubuntu missing after a failed Reload (state was clobbered)")
	}
}
