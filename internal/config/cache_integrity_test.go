package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheIntegrityYAMLAndExplicitOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integrity.yaml")
	if err := os.WriteFile(path, []byte("cache:\n  integrity: none\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Cache.Integrity != "none" {
		t.Fatalf("integrity opt-out lost: %+v", c.Cache)
	}
	for _, ex := range []*cliExplicit{nil, {}} {
		merged := MergeConfigsWithExplicit(&Config{Cache: CacheConfig{Integrity: "sha256"}}, c, ex)
		if merged.Cache.Integrity != "none" {
			t.Fatal("merge silently re-enabled integrity")
		}
	}
	for _, valid := range []string{"", "sha256", "none"} {
		if err := ValidateCacheIntegrity(valid); err != nil {
			t.Fatal(err)
		}
	}
	if ValidateCacheIntegrity("sha257") == nil {
		t.Fatal("unknown checksum policy accepted")
	}
}
