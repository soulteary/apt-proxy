package proxy

import (
	"fmt"
	"io"
	"sync"

	"github.com/soulteary/apt-proxy/internal/resource"
)

// admittedDownloadBody retains a known-length upstream's active slot until
// its body closes. It caps bytes even when a custom transport misreports size.
type admittedDownloadBody struct {
	io.ReadCloser
	lease     *resource.DownloadLease
	max, read int64
	once      sync.Once
	closeErr  error
}

func (b *admittedDownloadBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	remaining := b.max - b.read
	// Probe at most one byte past the cap; never expose that extra byte.
	if remaining < int64(len(p)) {
		p = p[:int(remaining)+1]
	}
	n, err := b.ReadCloser.Read(p)
	if int64(n) > remaining {
		b.read += remaining
		return int(remaining), fmt.Errorf("%w: streamed body", resource.ErrObjectTooLarge)
	}
	b.read += int64(n)
	return n, err
}
func (b *admittedDownloadBody) Close() error {
	b.once.Do(func() { b.closeErr = b.ReadCloser.Close(); b.lease.Close() })
	return b.closeErr
}
