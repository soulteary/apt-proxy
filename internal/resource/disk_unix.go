//go:build linux || darwin

package resource

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func availableBytes(path string) (int64, error) {
	for {
		var stat unix.Statfs_t
		err := unix.Statfs(path, &stat)
		if err == nil {
			return saturatingFree(stat.Bavail, uint64(stat.Bsize)), nil
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
