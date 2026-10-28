//go:build unix

package sharedcache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type plainCache struct {
	httpcache.ExtendedCache
	marker time.Time
}

func (c *plainCache) StaleAt(string) (time.Time, bool) { return c.marker, !c.marker.IsZero() }

func TestLockedCacheFailsClosedWhenCoordinationDisappears(t *testing.T) {
	dir := t.TempDir()
	c := newSharedCache(t, t.TempDir(), dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	key := "GET:http://example/pkg"
	res := sharedResource(time.Now())
	defer res.Close()
	if _, err := c.Header(key); err == nil {
		t.Fatal("header bypassed failed lock")
	}
	if _, err := c.Retrieve(key); err == nil {
		t.Fatal("retrieve bypassed failed lock")
	}
	if err := c.Store(res, key); err == nil {
		t.Fatal("store bypassed failed lock")
	}
	if err := c.Freshen(res, key); err == nil {
		t.Fatal("freshen bypassed failed lock")
	}
	c.Invalidate(key)
	if !c.failed[key].Equal(failedMarkerTime) {
		t.Fatal("failed invalidation was not retained")
	}
	if at, ok := c.StaleAt(key); !ok || !at.Equal(failedMarkerTime) {
		t.Fatal("stale lookup failed open")
	}
	if got := c.StaleSnapshot(key); !got[key].Equal(failedMarkerTime) {
		t.Fatal("snapshot failed open")
	}
	if err := c.Purge(); err == nil {
		t.Fatal("live shared cache permits purge")
	}
	if got := c.Cleanup(); got != (httpcache.CleanupResult{}) {
		t.Fatal("shared cache ran local cleanup")
	}
}

func TestLockedCacheHeaderAndBackendMarkers(t *testing.T) {
	c := newSharedCache(t, t.TempDir(), t.TempDir())
	key := "GET:http://example/pkg"
	res := sharedResource(time.Now())
	defer res.Close()
	if err := c.Store(res, key, key); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Header(key); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	c.ExtendedCache = &plainCache{marker: at}
	if got := c.backendStaleSnapshot([]string{key}); !got[key].Equal(at) {
		t.Fatal("legacy marker fallback lost timestamp")
	}
	c.ExtendedCache = &struct{ httpcache.ExtendedCache }{}
	if got := c.backendStaleSnapshot([]string{key}); len(got) != 0 {
		t.Fatal("unsupported markers invented")
	}
	c.recordFailure(key, at)
	if got, ok := c.staleAtLocked(key); !ok || !got.Equal(at) {
		t.Fatal("in-memory failure lost")
	}
}

func TestMarkerValidationAndPersistenceFailure(t *testing.T) {
	c, err := NewFileCoordinator(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"invalid", time.Time{}.Format(time.RFC3339Nano), string(make([]byte, 129))} {
		if err := os.WriteFile(c.markerPath("k"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.readMarker("k"); err == nil {
			t.Fatal("invalid marker accepted")
		}
	}
	if err := os.Remove(c.markerPath("k")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(c.markerPath("k"), 0700); err != nil {
		t.Fatal(err)
	}
	locked := &LockedCache{Coordinator: c}
	locked.invalidateLocked("k", time.Now())
	if got, ok := locked.staleAtLocked("k"); !ok || !got.Equal(failedMarkerTime) {
		t.Fatal("corrupt marker did not fail closed")
	}
	if !locked.failed["k"].Equal(failedMarkerTime) {
		t.Fatal("failed publication not recorded")
	}
	// A missing directory is independent of permission differences under root.
	gone := &FileCoordinator{root: filepath.Join(t.TempDir(), "missing")}
	if err := gone.writeMarker("k", time.Now()); err == nil {
		t.Fatal("missing marker directory accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Acquire(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire: %v", err)
	}
	lease, err := c.Acquire(context.Background(), "live")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Context().Err(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(lease.Context().Err(), context.Canceled) {
		t.Fatal("closed lease context remains active")
	}
}

func TestCoordinatorRejectsNonPrivateAndSymlinkRoots(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("not directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileCoordinator(file); err == nil {
		t.Fatal("file accepted as coordination directory")
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileCoordinator(target); err == nil {
		t.Fatal("world writable coordination directory accepted")
	}
	if err := os.Chmod(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileCoordinator(link); err == nil {
		t.Fatal("symlink coordination directory accepted")
	}
}

func TestCoordinatorRejectsRelativeDirectory(t *testing.T) {
	if _, err := NewFileCoordinator("relative"); err == nil {
		t.Fatal("relative lock root accepted")
	}
}
