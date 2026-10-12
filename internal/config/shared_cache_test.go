package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/soulteary/apt-proxy/internal/sharedcache"
)

func TestSharedCacheYAMLMergeAndBackendValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.yaml")
	if err := os.WriteFile(path, []byte(`shared_cache:
  repositories:
    - id: public
      bases: ["https://primary.example/ubuntu/", "https://secondary.example/archive/"]
  max_index_mb: 16
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SharedCache == nil || cfg.SharedCache.MaxIndexMB != 16 || len(cfg.SharedCache.Repositories) != 1 || len(cfg.SharedCache.Repositories[0].Bases) != 2 {
		t.Fatalf("shared YAML lost: %+v", cfg.SharedCache)
	}
	for _, merged := range []*Config{MergeConfigs(cfg, &Config{}), MergeConfigsWithExplicit(cfg, &Config{}, &cliExplicit{})} {
		if merged.SharedCache != cfg.SharedCache {
			t.Fatal("CLI defaults erased shared policy")
		}
	}
	override := &Config{SharedCache: &sharedcache.Config{Repositories: []sharedcache.Repository{{ID: "different", Bases: []string{"https://other.example/ubuntu/"}}}}}
	for _, merged := range []*Config{MergeConfigs(cfg, override), MergeConfigsWithExplicit(cfg, override, &cliExplicit{})} {
		if merged.SharedCache != override.SharedCache {
			t.Fatal("explicit shared policy not applied")
		}
	}
	cfg.Storage.Backend = StorageBackendS3
	cfg.SharedCache.CoordinationDir = t.TempDir()
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("S3 incorrectly accepted disk coordination")
	}
}
