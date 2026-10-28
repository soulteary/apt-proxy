//go:build linux || darwin

package resource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAvailableBytesUsesNearestAncestorAndRejectsInvalidPath(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing", "staging")} {
		free, err := availableBytes(path)
		if err != nil || free <= 0 {
			t.Fatalf("capacity(%q)=%d, %v", path, free, err)
		}
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := availableBytes(filepath.Join(file, "child")); err == nil {
		t.Fatal("not-a-directory path accepted")
	}
}
