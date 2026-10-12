//go:build linux || darwin

package resource

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func availableBytes(path string) (int64, error) {
	for {
		var stat unix.Statfs_t
		err := unix.Statfs(path, &stat)
		if err == nil {
			return statfsFreeBytes(stat)
		}
		if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, err
		}
		path = parent
	}
}

func statfsFreeBytes(stat unix.Statfs_t) (int64, error) {
	if stat.Bsize <= 0 {
		return 0, errors.New("filesystem block size must be positive")
	}
	return saturatingFree(stat.Bavail, uint64(stat.Bsize)), nil
}
