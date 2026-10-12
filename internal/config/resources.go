package config

import (
	"fmt"
	"math"
)

// ResourceConfig applies to every repository download and temporary cache
// copy. Zero selects bounded defaults; configured through YAML resources.
type ResourceConfig struct {
	MaxObjectMB        int64 `yaml:"max_object_mb"`
	TotalTempMB        int64 `yaml:"total_temp_mb"`
	MaxActiveDownloads int   `yaml:"max_active_downloads"`
	MinFreeMB          int64 `yaml:"min_free_mb"`
}

func ValidateResourceConfig(c ResourceConfig) error {
	if c.MaxObjectMB < 0 || c.TotalTempMB < 0 || c.MaxActiveDownloads < 0 || c.MinFreeMB < 0 {
		return fmt.Errorf("resource limits cannot be negative")
	}
	for _, n := range []int64{c.MaxObjectMB, c.TotalTempMB, c.MinFreeMB} {
		if n > math.MaxInt64/(1<<20) {
			return fmt.Errorf("resource limits overflow internal units")
		}
	}
	return nil
}
func mergeResourceConfig(dst *ResourceConfig, src ResourceConfig) {
	if src.MaxObjectMB != 0 {
		dst.MaxObjectMB = src.MaxObjectMB
	}
	if src.TotalTempMB != 0 {
		dst.TotalTempMB = src.TotalTempMB
	}
	if src.MaxActiveDownloads != 0 {
		dst.MaxActiveDownloads = src.MaxActiveDownloads
	}
	if src.MinFreeMB != 0 {
		dst.MinFreeMB = src.MinFreeMB
	}
}

// ValidateResourceBackend keeps S3 cache uploads within its bounded single-PUT
// strategy. Disk storage can use a larger explicitly configured object cap.
func ValidateResourceBackend(c ResourceConfig, backend string) error {
	if err := ValidateResourceConfig(c); err != nil {
		return err
	}
	if backend == StorageBackendS3 && c.MaxObjectMB > 5*1024 {
		return fmt.Errorf("S3 resources.max_object_mb cannot exceed 5120 for bounded single PUT")
	}
	return nil
}
