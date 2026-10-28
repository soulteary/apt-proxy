package system

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStatsCacheReusesUnexpiredSnapshot(t *testing.T) {
	memStatsMu.Lock()
	saved := memStatsCache
	memStatsCache = memSnapshot{alloc: 123, goroutine: "7", expiresAt: time.Now().Add(time.Hour)}
	memStatsMu.Unlock()
	defer func() { memStatsMu.Lock(); memStatsCache = saved; memStatsMu.Unlock() }()
	if bytes, gor := GetMemoryUsageAndGoroutine(); bytes != 123 || gor != "7" {
		t.Fatalf("cache miss: %d %s", bytes, gor)
	}
}

func TestDiskAvailableDefaultsAndMissingPath(t *testing.T) {
	// A deleted/nonexistent mount target must be reported as an error.
	if _, err := DiskAvailable(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing filesystem accepted")
	}
	if _, err := DiskAvailable(); err != nil {
		t.Fatal(err)
	}
	if _, err := DiskAvailable(""); err != nil {
		t.Fatal(err)
	}
}

func TestDiskAvailableReportsDeletedWorkingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "working")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := DiskAvailable(); err == nil {
		t.Fatal("deleted working directory accepted")
	}
}
