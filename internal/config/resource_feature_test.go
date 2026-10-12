package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestResourcesYAMLMergeAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources.yaml")
	if err := os.WriteFile(path, []byte("resources:\n  max_object_mb: 512\n  total_temp_mb: 2048\n  max_active_downloads: 8\n  min_free_mb: 64\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ResourceConfig{MaxObjectMB: 512, TotalTempMB: 2048, MaxActiveDownloads: 8, MinFreeMB: 64}
	if loaded.Resources != want {
		t.Fatalf("YAML resources=%+v", loaded.Resources)
	}
	for _, explicit := range []bool{false, true} {
		var ex *cliExplicit
		if explicit {
			ex = &cliExplicit{}
		}
		merged := MergeConfigsWithExplicit(loaded, &Config{Resources: ResourceConfig{MaxActiveDownloads: 4}}, ex)
		if merged.Resources.MaxActiveDownloads != 4 || merged.Resources.TotalTempMB != 2048 {
			t.Fatalf("resource merge=%+v", merged.Resources)
		}
	}
	for _, invalid := range []ResourceConfig{{MaxObjectMB: -1}, {TotalTempMB: -1}, {MaxActiveDownloads: -1}, {MinFreeMB: -1}, {TotalTempMB: math.MaxInt64}, {MinFreeMB: math.MaxInt64}} {
		if ValidateResourceConfig(invalid) == nil {
			t.Fatalf("invalid resources accepted: %+v", invalid)
		}
	}
}

func TestResourceSinglePutCapOnlyAppliesToS3(t *testing.T) {
	if err := ValidateResourceBackend(ResourceConfig{MaxObjectMB: 5120}, StorageBackendS3); err != nil {
		t.Fatal(err)
	}
	if err := ValidateResourceBackend(ResourceConfig{MaxObjectMB: 5121}, StorageBackendS3); err == nil {
		t.Fatal("S3 multipart-sized object limit accepted")
	}
	if err := ValidateResourceBackend(ResourceConfig{MaxObjectMB: 5121}, StorageBackendDisk); err != nil {
		t.Fatal(err)
	}
}
