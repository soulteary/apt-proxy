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

func TestDownloadLegacyMerge(t *testing.T) {
	base := &Config{Download: DownloadConfig{MaxSpoolMB: 32, TotalSpoolMB: 64, TimeoutSeconds: 15}}
	cases := []struct {
		name     string
		override DownloadConfig
		want     DownloadConfig
	}{
		{"zero preserves base", DownloadConfig{}, base.Download},
		{"max spool", DownloadConfig{MaxSpoolMB: 8}, DownloadConfig{MaxSpoolMB: 8, TotalSpoolMB: 64, TimeoutSeconds: 15}},
		{"total spool", DownloadConfig{TotalSpoolMB: 128}, DownloadConfig{MaxSpoolMB: 32, TotalSpoolMB: 128, TimeoutSeconds: 15}},
		{"timeout", DownloadConfig{TimeoutSeconds: 30}, DownloadConfig{MaxSpoolMB: 32, TotalSpoolMB: 64, TimeoutSeconds: 30}},
		{"invalid values remain visible", DownloadConfig{MaxSpoolMB: -1, TotalSpoolMB: -2, TimeoutSeconds: -3}, DownloadConfig{MaxSpoolMB: -1, TotalSpoolMB: -2, TimeoutSeconds: -3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			override := &Config{Download: tc.override}
			for _, got := range []*Config{MergeConfigs(base, override), MergeConfigsWithExplicit(base, override, nil)} {
				if got.Download != tc.want {
					t.Fatalf("merged download=%+v, want %+v", got.Download, tc.want)
				}
			}
			if base.Download != cases[0].want || override.Download != tc.override {
				t.Fatal("merge mutated an input")
			}
		})
	}
}
