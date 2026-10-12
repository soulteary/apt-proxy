//go:build unix

package sharedcache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestSharedRecheckPreservesDataLockAndPeerStaleBarrier(t *testing.T) {
	peers, _, _ := newSharedPair(t)
	const key = "GET:http://origin.test/Packages"
	if err := peers[0].Store(sharedResource(time.Now()), key); err != nil {
		t.Fatal(err)
	}
	assertSharedRecheckWaitsForDataLock(t, peers, key)
	assertSharedRecheckPreservesPeerBarrier(t, peers, key)
}

func assertSharedRecheckWaitsForDataLock(t *testing.T, peers []*LockedCache, key string) {
	t.Helper()
	// Hold exactly the data lock a recheck must acquire. Its read cannot
	// bypass a peer's publication/invalidation critical section.
	release, err := peers[0].hold(context.Background(), []string{key})
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	unlock := func() { once.Do(release) }
	defer unlock()
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		res, err := peers[1].RetrieveForRecheck(key)
		if res != nil {
			_ = res.Close()
		}
		result <- err
	}()
	<-started
	select {
	case err := <-result:
		t.Fatalf("recheck bypassed peer data lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recheck did not release after data publication")
	}
}

func assertSharedRecheckPreservesPeerBarrier(t *testing.T, peers []*LockedCache, key string) {
	t.Helper()
	peers[0].Invalidate(key)
	res, err := peers[1].RetrieveForRecheck(key)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Close()
	if !res.IsStale() {
		t.Fatal("recheck bypassed persisted peer invalidation")
	}
	if stats := peers[1].Stats(); stats.HitCount != 0 || stats.MissCount != 0 {
		t.Fatalf("shared recheck counted another public lookup: %+v", stats)
	}
	before := peers[1].Stats().MissCount
	for range 3 {
		if _, err := peers[1].RetrieveForRecheck("absent"); !errors.Is(err, httpcache.ErrNotFoundInCache) {
			t.Fatal(err)
		}
	}
	if after := peers[1].Stats().MissCount; after != before {
		t.Fatalf("shared rechecks inflated native misses: %d -> %d", before, after)
	}
}
