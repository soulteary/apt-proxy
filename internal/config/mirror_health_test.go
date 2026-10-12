package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
)

func TestMirrorHealthYAMLAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apt-proxy.yaml")
	if err := os.WriteFile(path, []byte(`mirror_health:
  enabled: true
  failure_threshold: 3
  probe_timeout_seconds: 4
  repositories:
    - id: ubuntu
      mirrors: ["https://primary.example/ubuntu/", "https://secondary.example/archive/"]
      public_query_keys: [download]
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MirrorHealth == nil || !cfg.MirrorHealth.Enabled || cfg.MirrorHealth.FailureThreshold != 3 || cfg.MirrorHealth.Repositories[0].PublicQueryKeys[0] != "download" {
		t.Fatalf("YAML health lost: %+v", cfg.MirrorHealth)
	}
	for _, merged := range []*Config{MergeConfigs(cfg, &Config{}), MergeConfigsWithExplicit(cfg, &Config{}, &cliExplicit{})} {
		if merged.MirrorHealth != cfg.MirrorHealth {
			t.Fatal("CLI defaults erased YAML-only mirror health")
		}
	}
	disabled := &Config{MirrorHealth: &mirrorhealth.Config{Enabled: false}}
	if merged := MergeConfigs(cfg, disabled); merged.MirrorHealth.Enabled {
		t.Fatal("explicit disable lost")
	}
}
