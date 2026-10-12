//go:build linux

package resource

import (
	"testing"

	"golang.org/x/sys/unix"
)

// Bsize is signed on Linux; macOS exposes an unsigned block size.
func TestNegativeStatfsBlockSizeRefusesAdmission(t *testing.T) {
	assertInvalidStatfsRefusesAdmission(t, unix.Statfs_t{Bavail: 1, Bsize: -1})
}
