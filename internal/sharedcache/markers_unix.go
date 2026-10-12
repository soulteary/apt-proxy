//go:build unix

package sharedcache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Each key has its own journal entry. A process must not publish a snapshot
// of its in-memory map: that snapshot cannot include another live process's
// invalidations. Callers hold the corresponding data lock for these methods.
func (c *FileCoordinator) markerPath(key string) string {
	h := sha256.Sum256([]byte("stale:" + key))
	return filepath.Join(c.root, hex.EncodeToString(h[:])+".stale")
}

func (c *FileCoordinator) readMarker(key string) (time.Time, bool, error) {
	name := c.markerPath(key)
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128 {
		return time.Time{}, false, fmt.Errorf("invalid shared cache stale marker")
	}
	b, err := io.ReadAll(io.LimitReader(f, 129))
	if err != nil || len(b) > 128 {
		return time.Time{}, false, fmt.Errorf("cannot read shared cache stale marker: %w", err)
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil || at.IsZero() {
		return time.Time{}, false, fmt.Errorf("invalid shared cache stale marker timestamp")
	}
	return at, true, nil
}

func (c *FileCoordinator) writeMarker(key string, at time.Time) error {
	f, err := os.CreateTemp(c.root, ".stale-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer func() { _ = os.Remove(temporary) }()
	_, writeErr := io.WriteString(f, at.UTC().Format(time.RFC3339Nano)+"\n")
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := f.Close(); writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		return writeErr
	}
	if err := os.Rename(temporary, c.markerPath(key)); err != nil {
		return err
	}
	dir, err := os.Open(c.root)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
