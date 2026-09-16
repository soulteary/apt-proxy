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
	"os"
	"path/filepath"
	"testing"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// overrideUbuntuYAML reconfigures a built-in rather than adding a new
// distribution. That keeps these tests free of background benchmark
// goroutines: withTestMirrors gives Ubuntu an explicit mirror, so rewriter
// construction resolves inline. A custom type has no mirror flag, would elect
// its mirror by benchmark, and would leave a goroutine logging after the test.
const overrideUbuntuYAML = `
distributions:
  - id: ubuntu
    name: Ubuntu
    type: 1
    url_pattern: "/ubuntu/(.+)$"
    benchmark_url: "loaded-from-file"
    cache_rules:
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "mirrors.example.com/ubuntu/"
`

// customDistroYAML registers a distribution outside the built-in range, the
// shape the README documents.
const customDistroYAML = `
distributions:
  - id: deepin
    name: Deepin
    type: 6
    url_pattern: "/deepin/(.+)$"
    benchmark_url: "dists/apricot/Release"
    cache_rules:
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "community-packages.deepin.com/deepin/"
`

func writeAt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The server must find a distributions.yaml sitting on the documented search
// path with no --distributions-config. Both call sites used to guard Reload on
// a non-empty path, which made Loader.Load's search list unreachable from the
// server: such a file was silently ignored.
func TestServerFindsDistributionsConfigOnSearchPath(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "config", "distributions.yaml"), overrideUbuntuYAML)
	t.Chdir(dir)

	srv, err := NewServer(withTestMirrors(&config.Config{
		CacheDir: t.TempDir(),
		Listen:   "127.0.0.1:0",
	}))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })

	d, ok := srv.registry.GetByID("ubuntu")
	if !ok {
		t.Fatal("ubuntu is missing from the registry")
	}
	if d.BenchmarkURL != "loaded-from-file" {
		t.Errorf("BenchmarkURL = %q, want the value from the file on the search path", d.BenchmarkURL)
	}
	if got := srv.registry.ConfigPath(); got != filepath.Join("config", "distributions.yaml") {
		t.Errorf("ConfigPath() = %q, want the file found on the search path", got)
	}
}

// Nothing to find: built-ins stand, startup succeeds, and no stale path is
// reported.
func TestServerWithoutDistributionsConfigKeepsBuiltins(t *testing.T) {
	t.Chdir(t.TempDir())

	srv, err := NewServer(withTestMirrors(&config.Config{
		CacheDir: t.TempDir(),
		Listen:   "127.0.0.1:0",
	}))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })

	d, ok := srv.registry.GetByID("ubuntu")
	if !ok {
		t.Fatal("built-in distributions must survive a search that finds nothing")
	}
	if d.BenchmarkURL == "loaded-from-file" {
		t.Error("a file was loaded from somewhere it should not have been")
	}
	if got := srv.registry.ConfigPath(); got != "" {
		t.Errorf("ConfigPath() = %q, want empty for built-ins only", got)
	}
}

// A malformed file must not take the server down or strip the built-ins.
func TestServerSurvivesUnparseableDistributionsConfig(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "config", "distributions.yaml"), "distributions: [ this is not: valid yaml")
	t.Chdir(dir)

	srv, err := NewServer(withTestMirrors(&config.Config{
		CacheDir: t.TempDir(),
		Listen:   "127.0.0.1:0",
	}))
	if err != nil {
		t.Fatalf("NewServer must survive a broken distributions.yaml: %v", err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })

	if _, ok := srv.registry.GetByID("ubuntu"); !ok {
		t.Error("built-ins must remain after a parse failure")
	}
}

// The search order itself, exercised at the registry level so a custom
// distribution can be used without electing a mirror. Each entry in
// Loader.Load's list must be reachable, and the first match must win.
func TestRegistrySearchPathOrder(t *testing.T) {
	for _, relative := range []string{
		filepath.Join("config", "distributions.yaml"),
		"distributions.yaml",
	} {
		t.Run(relative, func(t *testing.T) {
			dir := t.TempDir()
			writeAt(t, filepath.Join(dir, relative), customDistroYAML)
			t.Chdir(dir)

			reg := distro.NewBuiltinRegistry()
			if err := reg.Reload(""); err != nil {
				t.Fatalf("Reload(%q): %v", "", err)
			}

			d, ok := reg.GetByType(6)
			if !ok {
				t.Fatalf("distribution from %s was not registered", relative)
			}
			if d.ID != "deepin" {
				t.Errorf("registered %q, want %q", d.ID, "deepin")
			}
			if got := reg.ConfigPath(); got != relative {
				t.Errorf("ConfigPath() = %q, want %q", got, relative)
			}
		})
	}
}

// ./config/distributions.yaml is searched before ./distributions.yaml.
func TestRegistrySearchPrefersConfigDir(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "config", "distributions.yaml"), customDistroYAML)
	writeAt(t, filepath.Join(dir, "distributions.yaml"), overrideUbuntuYAML)
	t.Chdir(dir)

	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(""); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := reg.ConfigPath(); got != filepath.Join("config", "distributions.yaml") {
		t.Errorf("ConfigPath() = %q, want the config/ copy to win", got)
	}
}

// An explicit path still wins and is not restricted to the search list.
func TestRegistryExplicitPathWinsOverSearch(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "config", "distributions.yaml"), overrideUbuntuYAML)
	explicit := filepath.Join(dir, "elsewhere", "custom.yaml")
	writeAt(t, explicit, customDistroYAML)
	t.Chdir(dir)

	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(explicit); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if _, ok := reg.GetByType(6); !ok {
		t.Error("the explicitly named file was not the one loaded")
	}
	if got := reg.ConfigPath(); got != explicit {
		t.Errorf("ConfigPath() = %q, want %q", got, explicit)
	}
}
