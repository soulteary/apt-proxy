package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/soulteary/apt-proxy/internal/metalink"
)

func TestMetalinkYAMLAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apt-proxy.yaml")
	if err := os.WriteFile(path, []byte(`metalink:
  public_url: http://cache.example:3142
  source_url: https://mirrors.centos.org/metalink
  allowed_mirror_hosts: [mirrors.example.org]
  refresh_seconds: 600
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Metalink == nil || cfg.Metalink.PublicURL != "http://cache.example:3142" || cfg.Metalink.SourceURL != "https://mirrors.centos.org/metalink" {
		t.Fatalf("metalink YAML lost: %+v", cfg.Metalink)
	}
	if _, err := metalink.New(cfg.Metalink, cfg.UpstreamAccess); err != nil {
		t.Fatal(err)
	}
	for _, merged := range []*Config{MergeConfigs(cfg, &Config{}), MergeConfigsWithExplicit(cfg, &Config{}, &cliExplicit{})} {
		if merged.Metalink != cfg.Metalink {
			t.Fatal("CLI defaults erased YAML-only metalink configuration")
		}
	}
	override := &Config{Metalink: &metalink.Config{PublicURL: "https://new-cache.example"}}
	for _, merged := range []*Config{MergeConfigs(cfg, override), MergeConfigsWithExplicit(cfg, override, &cliExplicit{})} {
		if merged.Metalink != override.Metalink {
			t.Fatal("metalink override lost")
		}
	}
}

func TestMetalinkInvalidConfigFailsValidation(t *testing.T) {
	cfg := &Config{Listen: "127.0.0.1:3142", CacheDir: t.TempDir()}
	cfg.Metalink = &metalink.Config{PublicURL: "http://user:password@cache.example", AllowedMirrorHosts: []string{"mirror.example"}}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("accepted a credential-bearing public metalink origin")
	}
}
