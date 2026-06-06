package system

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// DirSize returns the total size in bytes of all files under path.
// Uses filepath.WalkDir for fewer syscalls and better performance (Go 1.16+).
func DirSize(path string) (uint64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	if size < 0 {
		size = 0
	}
	return uint64(size), err
}

// ByteCountDecimal formats b as human-readable size (1000-based: kB, MB, …).
// Use ByteCountBinary for filesystem-style 1024-based output.
func ByteCountDecimal(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// ByteCountBinary formats b as human-readable size (1024-based: KiB, MiB, …,
// rendered as KB/MB/GB to match common storage conventions). Used by the API
// stats endpoint so disk usage figures line up with `du` / filesystem tools.
func ByteCountBinary(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
