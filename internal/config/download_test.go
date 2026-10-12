package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadYAMLAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("download:\n  max_spool_mb: 32\n  total_spool_mb: 64\n  timeout_seconds: 15\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := MergeConfigsWithExplicit(cfg, &Config{}, &cliExplicit{})
	if got.Download != (DownloadConfig{MaxSpoolMB: 32, TotalSpoolMB: 64, TimeoutSeconds: 15}) {
		t.Fatalf("download YAML lost during merge: %+v", got.Download)
	}
}
func TestValidateDownloadLimits(t *testing.T) {
	for _, limits := range []DownloadConfig{{MaxSpoolMB: -1}, {TotalSpoolMB: -1}, {TimeoutSeconds: -1}, {MaxSpoolMB: math.MaxInt64}, {TotalSpoolMB: math.MaxInt64}, {TimeoutSeconds: math.MaxInt64}} {
		cfg := &Config{Listen: ":8080", CacheDir: t.TempDir(), Download: limits}
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("invalid limits accepted: %+v", limits)
		}
	}
	cfg := &Config{Listen: ":8080", CacheDir: t.TempDir()}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("zero/default limits rejected: %v", err)
	}
}
