package system

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestByteCountDecimal(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{42, "42 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{1500, "1.5 kB"},
		{1_000_000, "1.0 MB"},
		{2_500_000, "2.5 MB"},
		{1_000_000_000, "1.0 GB"},
	}
	for _, c := range cases {
		if got := ByteCountDecimal(c.in); got != c.want {
			t.Errorf("ByteCountDecimal(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()

	mustWrite := func(rel string, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	mustWrite("a.txt", "hello")     // 5
	mustWrite("nested/b.txt", "hi") // 2
	mustWrite("nested/deep/c.bin", strings.Repeat("x", 100))

	got, err := DirSize(dir)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	want := uint64(5 + 2 + 100)
	if got != want {
		t.Errorf("DirSize = %d, want %d", got, want)
	}
}

func TestDirSizeMissingPath(t *testing.T) {
	_, err := DirSize(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Errorf("expected error for missing path, got nil")
	}
}

// TestDiskAvailable just checks the call returns non-zero and no error for
// the temp directory. The exact value is platform-specific.
func TestDiskAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("DiskAvailable on Windows uses the fallback implementation; skip")
	}
	dir := t.TempDir()
	avail, err := DiskAvailable(dir)
	if err != nil {
		t.Fatalf("DiskAvailable: %v", err)
	}
	// On any reasonable test environment we expect the temp dir to have at
	// least 1 byte free. Don't assert beyond that.
	if avail == 0 {
		t.Errorf("expected non-zero available bytes for %s", dir)
	}
}

func TestGetMemoryUsageAndGoroutine(t *testing.T) {
	alloc, gor := GetMemoryUsageAndGoroutine()
	if alloc == 0 {
		t.Error("expected non-zero alloc")
	}
	if gor == "" {
		t.Error("expected non-empty goroutine count string")
	}
}
