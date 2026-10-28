package distro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryReportsDiscoveredConfigurationPath(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := os.Mkdir("config", 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("distributions: []\n")
	if err := os.WriteFile("config/distributions.yaml", data, 0600); err != nil {
		t.Fatal(err)
	}
	r := NewBuiltinRegistry()
	if r.ConfigPath() != "" {
		t.Fatal("builtin registry reports a file")
	}
	if err := r.Reload(""); err != nil {
		t.Fatal(err)
	}
	if r.ConfigPath() != "config/distributions.yaml" {
		t.Fatalf("path %q", r.ConfigPath())
	}
}
func TestDirectConfigCompileFailuresDoNotRegister(t *testing.T) {
	for _, c := range []DistributionConfig{{ID: "bad", Type: 6, URLPattern: "["}, {ID: "bad", Type: 6, URLPattern: ".*", CacheRules: []CacheRuleConfig{{Pattern: "["}}}} {
		r := NewRegistry()
		if err := r.LoadFromConfig(&c); err == nil {
			t.Fatal("invalid regex accepted")
		}
		if len(r.GetAll()) != 0 {
			t.Fatal("partially registered invalid config")
		}
	}
}
func TestReloadRegistrationConflictIsReportedWithConfigPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "distributions.yaml")
	data := `distributions:
  - id: conflicting
    name: Conflicting
    type: 1
    url_pattern: '/custom/(.+)$'
    benchmark_url: Release
`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewBuiltinRegistry()
	if err := r.Reload(p); err == nil || !strings.Contains(err.Error(), "registering conflicting") {
		t.Fatalf("conflict: %v", err)
	}
	if r.ConfigPath() != p {
		t.Fatal("effective config path lost")
	}
	if _, ok := r.GetByID("ubuntu"); !ok {
		t.Fatal("builtins lost after conflict")
	}
}
func TestBuiltinConflictsSurfaceAsProgrammerErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&RegisteredDistribution{ID: "conflict", Type: TypeUbuntu}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("builtin conflict was silently dropped")
		}
	}()
	RegisterBuiltins(r)
}
