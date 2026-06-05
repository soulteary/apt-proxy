//go:build !unix

package system

import "errors"

// DiskAvailable returns the number of bytes available. On non-Unix platforms
// it returns an error so callers can show "N/A" or equivalent. The path
// argument is accepted for API parity with the Unix implementation.
func DiskAvailable(path ...string) (uint64, error) {
	_ = path
	return 0, errors.New("disk available not supported on this platform")
}
