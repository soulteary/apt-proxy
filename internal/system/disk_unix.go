//go:build unix

package system

import (
	"os"

	"golang.org/x/sys/unix"
)

// DiskAvailable returns the number of bytes available to the current user
// in the current working directory's filesystem. Unix-only.
func DiskAvailable() (uint64, error) {
	var stat unix.Statfs_t
	wd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	err = unix.Statfs(wd, &stat)
	if err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
