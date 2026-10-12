//go:build unix

package sharedcache

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/storage"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func newSharedPair(t *testing.T) ([]*LockedCache, string, string) {
	t.Helper()
	disk, locks := t.TempDir(), t.TempDir()
	return []*LockedCache{newSharedCache(t, disk, locks), newSharedCache(t, disk, locks)}, disk, locks
}

func newSharedCache(t *testing.T, disk, locks string) *LockedCache {
	t.Helper()
	backend, err := httpcache.NewDiskCacheWithConfig(disk, &httpcache.CacheConfig{Integrity: httpcache.IntegritySHA256})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	coordinator, err := NewFileCoordinator(locks)
	if err != nil {
		t.Fatal(err)
	}
	return &LockedCache{ExtendedCache: &storage.VerifiedCache{ExtendedCache: backend}, Coordinator: coordinator}
}

func sharedResource(at time.Time) *httpcache.Resource {
	r := httpcache.NewResourceBytes(http.StatusOK, []byte("complete"), http.Header{
		"Content-Length": {"8"}, "Cache-Control": {"public, max-age=3600"}, "Etag": {`"v1"`},
	})
	r.RequestTime = at
	return r
}

func assertSharedStale(t *testing.T, c *LockedCache, key string, want bool) {
	t.Helper()
	r, err := c.Retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if r.IsStale() != want {
		t.Fatalf("%s stale=%v, want %v", key, r.IsStale(), want)
	}
}

func TestSharedInvalidationPeersAndRestart(t *testing.T) {
	peers, disk, locks := newSharedPair(t)
	const key = "GET:http://mirror.example/package.deb"
	if err := peers[0].Store(sharedResource(time.Now()), key); err != nil {
		t.Fatal(err)
	}
	assertSharedStale(t, peers[1], key, false)
	peers[0].Invalidate(key)
	assertSharedStale(t, peers[1], key, true)
	if _, ok := peers[1].StaleAt(key); !ok {
		t.Fatal("peer did not see durable invalidation")
	}
	assertSharedStale(t, newSharedCache(t, disk, locks), key, true)
	if err := peers[1].Store(sharedResource(time.Now()), key); err != nil {
		t.Fatal(err)
	}
	assertSharedStale(t, peers[0], key, false)
}

func TestSharedInvalidationConcurrentKeysAndVariants(t *testing.T) {
	peers, disk, locks := newSharedPair(t)
	const count = 32
	keys := make([]string, count)
	var wg sync.WaitGroup
	for i := range keys {
		keys[i] = fmt.Sprintf("GET:http://mirror.example/package-%d.deb", i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			peers[i%2].Invalidate(keys[i])
		}(i)
	}
	wg.Wait()
	for _, c := range append(peers, newSharedCache(t, disk, locks)) {
		if snapshot := c.StaleSnapshot(keys...); len(snapshot) != count {
			t.Fatalf("invalidation overwritten by another instance: %d markers", len(snapshot))
		}
	}
	const base, variant = "GET:http://mirror.example/Packages", "GET:http://mirror.example/Packages:vary=en"
	started := time.Now()
	peers[0].Invalidate(base)
	if err := peers[1].Store(sharedResource(started), variant, base); err == nil {
		t.Fatal("pre-invalidation variant response republished")
	}
	if _, err := peers[0].Retrieve(variant); err == nil {
		t.Fatal("rejected store partially published its first key")
	}
	if err := peers[1].Store(sharedResource(time.Now()), variant, base); err != nil {
		t.Fatal(err)
	}
	assertSharedStale(t, peers[0], variant, false)
}

func TestSharedInvalidationFreshenOrdering(t *testing.T) {
	peers, _, _ := newSharedPair(t)
	const key = "GET:http://mirror.example/Packages"
	if err := peers[0].Store(sharedResource(time.Now()), key); err != nil {
		t.Fatal(err)
	}
	old, err := peers[1].Retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()
	old.RequestTime = time.Now()
	peers[0].Invalidate(key)
	old.Header().Set("Cache-Control", "public, max-age=7200")
	if err := peers[1].Freshen(old, key); err == nil {
		t.Fatal("old validation bypassed another instance's invalidation")
	}
	assertSharedStale(t, peers[1], key, true)
	old.RequestTime = time.Now()
	if err := peers[1].Freshen(old, key); err != nil {
		t.Fatal(err)
	}
	assertSharedStale(t, peers[0], key, false)
}

func TestSharedFreshenMismatchPublishesInvalidation(t *testing.T) {
	peers, _, _ := newSharedPair(t)
	const key = "GET:http://mirror.example/Packages"
	if err := peers[0].Store(sharedResource(time.Now()), key); err != nil {
		t.Fatal(err)
	}
	r, err := peers[0].Retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	r.RequestTime = time.Now()
	r.Header().Set("Etag", `"changed"`)
	if err := peers[0].Freshen(r, key); err != nil {
		t.Fatal(err)
	}
	assertSharedStale(t, peers[1], key, true)
}

type blockingSharedStore struct {
	httpcache.ExtendedCache
	started, resume chan struct{}
}

func (b *blockingSharedStore) Store(r *httpcache.Resource, keys ...string) error {
	close(b.started)
	<-b.resume
	return b.ExtendedCache.Store(r, keys...)
}

func TestSharedInvalidationWaitsForOlderStore(t *testing.T) {
	peers, _, _ := newSharedPair(t)
	const key = "GET:http://mirror.example/Packages"
	blocking := &blockingSharedStore{ExtendedCache: peers[0].ExtendedCache, started: make(chan struct{}), resume: make(chan struct{})}
	peers[0].ExtendedCache = blocking
	stored := make(chan error, 1)
	go func() { stored <- peers[0].Store(sharedResource(time.Now()), key) }()
	<-blocking.started
	invalidated := make(chan struct{})
	go func() { peers[1].Invalidate(key); close(invalidated) }()
	// A second coordinator must not acquire the writer's data lock, proving
	// the invalidation is waiting for the in-flight generation to finish.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if lease, err := peers[1].Coordinator.lock(ctx, "data:"+key); err == nil {
		_ = lease.Close()
		t.Fatal("active store did not retain the shared data lock")
	}
	close(blocking.resume)
	if err := <-stored; err != nil {
		t.Fatal(err)
	}
	select {
	case <-invalidated:
	case <-time.After(3 * time.Second):
		t.Fatal("invalidation deadlocked behind completed writer")
	}
	assertSharedStale(t, peers[0], key, true)
}

func TestSharedInvalidationMarkerReadFailsClosed(t *testing.T) {
	for _, damage := range []string{"corrupt", "symlink", "oversized"} {
		t.Run(damage, func(t *testing.T) {
			peers, _, _ := newSharedPair(t)
			const key = "GET:http://mirror.example/package.deb"
			if err := peers[0].Store(sharedResource(time.Now()), key); err != nil {
				t.Fatal(err)
			}
			name := peers[0].Coordinator.markerPath(key)
			var err error
			switch damage {
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside")
				if err = os.WriteFile(target, []byte(time.Now().Add(-time.Hour).Format(time.RFC3339Nano)), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(target, name)
			case "oversized":
				err = os.WriteFile(name, make([]byte, 129), 0600)
			default:
				err = os.WriteFile(name, []byte("partial"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertSharedStale(t, peers[1], key, true)
			if err := peers[1].Store(sharedResource(time.Now()), key); err == nil {
				t.Fatal("unreadable barrier permitted a cache publication")
			}
		})
	}
}
