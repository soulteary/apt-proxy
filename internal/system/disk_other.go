//go:build !unix

package system

import "errors"

// DiskAvailable returns the number of bytes available. On non-Unix platforms
// it returns an error so callers can show "N/A" or equivalent.
func DiskAvailable() (uint64, error) {
	return 0, errors.New("disk available not supported on this platform")
}
