//go:build unix

package sharedcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
	"golang.org/x/sys/unix"
)

// FileCoordinator uses kernel locks without expiry. A dead process loses
// ownership automatically; lock files are never unlinked while peers can use them.
type FileCoordinator struct{ root string }

func NewFileCoordinator(root string) (*FileCoordinator, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("coordination_dir must be absolute")
	}
	// This absolute directory is chosen by the operator, never an HTTP input.
	if err := os.MkdirAll(root, 0700); err != nil { // #nosec G703 -- intentional operator-selected absolute directory.
		return nil, err
	}
	info, err := os.Lstat(root) // #nosec G703 -- validate the operator-selected coordination directory itself.
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("coordination directory must be private and cannot be a symlink")
	}
	return &FileCoordinator{root: root}, nil
}
func (c *FileCoordinator) Acquire(ctx context.Context, key string) (httpcache.MissLease, error) {
	lease, err := c.lock(ctx, "miss:"+key)
	if err != nil {
		return nil, err
	}
	return lease, nil
}
func (c *FileCoordinator) lock(ctx context.Context, key string) (*fileLease, error) {
	h := sha256.Sum256([]byte(key))
	name := filepath.Join(c.root, hex.EncodeToString(h[:])+".lock")
	fd, err := unix.Open(name, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("invalid coordination lock file")
	}
	for {
		if err = ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			owned, cancel := context.WithCancel(ctx)
			return &fileLease{file: f, ctx: owned, cancel: cancel}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = f.Close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type fileLease struct {
	file   *os.File
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	err    error
}

func (l *fileLease) Context() context.Context { return l.ctx }
func (l *fileLease) Close() error {
	l.once.Do(func() { l.cancel(); l.err = l.file.Close() })
	return l.err
}
