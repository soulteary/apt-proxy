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
	"testing"
)

// host_pattern round-trips from YAML into a compiled matcher on the
// registered distribution.
func TestLoadFromConfigCompilesHostPattern(t *testing.T) {
	reg := NewRegistry()
	cfg := &DistributionConfig{
		ID:           "armbian",
		Name:         "Armbian",
		Type:         42,
		URLPattern:   `/armbian/(.+)$`,
		HostPattern:  `^apt\.armbian\.com(:\d+)?$`,
		BenchmarkURL: "dists/bookworm/main/binary-arm64/Release",
		Mirrors:      MirrorListConfig{Official: []string{"apt.armbian.com/"}},
	}
	if err := reg.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}

	d, ok := reg.GetByType(42)
	if !ok {
		t.Fatal("distribution not registered")
	}
	if d.HostPattern == nil {
		t.Fatal("HostPattern was not compiled")
	}
	for _, host := range []string{"apt.armbian.com", "apt.armbian.com:3142"} {
		if !d.HostPattern.MatchString(host) {
			t.Errorf("HostPattern should match %q", host)
		}
	}
	for _, host := range []string{"apt.armbian.com.evil.test", "armbian.com", "example.com"} {
		if d.HostPattern.MatchString(host) {
			t.Errorf("HostPattern must not match %q", host)
		}
	}
}

// host_pattern is optional, and a bad regex is rejected at load time rather
// than panicking later.
func TestHostPatternIsOptionalAndValidated(t *testing.T) {
	reg := NewRegistry()
	noHost := &DistributionConfig{
		ID: "nohost", Name: "No Host", Type: 43,
		URLPattern: `/nohost/(.+)$`, BenchmarkURL: "x",
	}
	if err := reg.LoadFromConfig(noHost); err != nil {
		t.Fatalf("host_pattern must stay optional: %v", err)
	}
	if d, _ := reg.GetByType(43); d.HostPattern != nil {
		t.Error("absent host_pattern should leave HostPattern nil")
	}

	bad := &DistributionConfig{
		ID: "bad", Name: "Bad", Type: 44,
		URLPattern: `/bad/(.+)$`, HostPattern: `^(unclosed`, BenchmarkURL: "x",
	}
	if err := reg.LoadFromConfig(bad); err == nil {
		t.Error("an invalid host_pattern regex must be rejected")
	}

	loader := NewLoader("")
	if err := loader.validateDistribution(&DistributionConfig{
		ID: "bad", Name: "Bad", URLPattern: `/bad/(.+)$`,
		HostPattern: `^(unclosed`, BenchmarkURL: "x",
	}); err == nil {
		t.Error("validateDistribution must reject an invalid host_pattern")
	}
}

// The built-in Debian entry carries the security host pattern so the classic
// sources.list form works without any configuration (issue #97).
func TestBuiltinDebianCarriesSecurityHostPattern(t *testing.T) {
	reg := NewBuiltinRegistry()
	d, ok := reg.GetByType(TypeDebian)
	if !ok {
		t.Fatal("debian not registered")
	}
	if d.HostPattern == nil {
		t.Fatal("built-in Debian should carry DebianSecurityHostPattern")
	}
	if !d.HostPattern.MatchString("security.debian.org") {
		t.Error("should match security.debian.org")
	}
	if d.HostPattern.MatchString("deb.debian.org") {
		t.Error("must not match the regular archive host")
	}
}

// A distributions.yaml file carrying host_pattern parses end to end.
func TestLoaderParsesHostPatternFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "distributions.yaml")
	yaml := `distributions:
  - id: armbian
    name: Armbian
    type: 42
    url_pattern: "/armbian/(.+)$"
    host_pattern: "^apt\\.armbian\\.com$"
    benchmark_url: "dists/bookworm/main/binary-arm64/Release"
    mirrors:
      official:
        - "apt.armbian.com/"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := NewLoader(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Distributions) != 1 {
		t.Fatalf("got %d distributions, want 1", len(cfg.Distributions))
	}
	if got, want := cfg.Distributions[0].HostPattern, `^apt\.armbian\.com$`; got != want {
		t.Errorf("HostPattern = %q, want %q", got, want)
	}
}

// An omitted host_pattern inherits the built-in matcher, so a pre-existing
// distributions.yaml does not silently disable Host routing.
func TestOmittedHostPatternInheritsBuiltin(t *testing.T) {
	reg := NewRegistry()
	if err := reg.LoadFromConfig(&DistributionConfig{
		ID: "debian", Name: "Debian", Type: TypeDebian,
		URLPattern: `/debian(-security)?/(.+)$`, BenchmarkURL: "x",
	}); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}
	d, _ := reg.GetByType(TypeDebian)
	if d.HostPattern == nil {
		t.Fatal("omitting host_pattern must inherit the built-in Debian matcher")
	}
	if !d.HostPattern.MatchString("security.debian.org") {
		t.Error("inherited pattern should match the security host")
	}

	// An explicit host_pattern still wins.
	reg2 := NewRegistry()
	if err := reg2.LoadFromConfig(&DistributionConfig{
		ID: "debian", Name: "Debian", Type: TypeDebian,
		URLPattern: `/debian/(.+)$`, HostPattern: `^apt\.internal$`, BenchmarkURL: "x",
	}); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}
	d2, _ := reg2.GetByType(TypeDebian)
	if !d2.HostPattern.MatchString("apt.internal") || d2.HostPattern.MatchString("security.debian.org") {
		t.Error("an explicit host_pattern must replace the built-in")
	}

	// Types without a built-in matcher stay nil.
	if BuiltinHostPattern(TypeAlpine) != nil {
		t.Error("alpine should have no built-in host pattern")
	}
}
