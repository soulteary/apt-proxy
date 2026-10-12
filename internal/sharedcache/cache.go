package sharedcache

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// LockedCache protects each header/body operation across instances. Miss leases
// use a distinct lock domain, so a leader can commit while owning its miss lock.
type LockedCache struct {
	httpcache.ExtendedCache
	Coordinator *FileCoordinator
	failedMu    sync.Mutex
	failed      map[string]time.Time
}

var failedMarkerTime = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)

func (c *LockedCache) hold(ctx context.Context, keys []string) (func(), error) {
	ordered := append([]string(nil), keys...)
	sort.Strings(ordered)
	var leases []*fileLease
	closeAll := func() {
		for i := len(leases) - 1; i >= 0; i-- {
			_ = leases[i].Close()
		}
	}
	previous := ""
	for i, key := range ordered {
		if i > 0 && key == previous {
			continue
		}
		previous = key
		lease, err := c.Coordinator.lock(ctx, "data:"+key)
		if err != nil {
			closeAll()
			return nil, err
		}
		leases = append(leases, lease)
	}
	return closeAll, nil
}
func (c *LockedCache) Header(key string) (httpcache.Header, error) {
	release, err := c.hold(context.Background(), []string{key})
	if err != nil {
		return httpcache.Header{}, err
	}
	defer release()
	return c.ExtendedCache.Header(key)
}
func (c *LockedCache) Retrieve(key string) (*httpcache.Resource, error) {
	return c.retrieve(key, false)
}

// RetrieveForRecheck preserves verification and ordering while suppressing
// repeated native cache counts during coordinated fill retries.
func (c *LockedCache) RetrieveForRecheck(key string) (*httpcache.Resource, error) {
	return c.retrieve(key, true)
}

func (c *LockedCache) retrieve(key string, recheck bool) (*httpcache.Resource, error) {
	release, err := c.hold(context.Background(), []string{key})
	if err != nil {
		return nil, err
	}
	defer release()
	retrieve := c.ExtendedCache.Retrieve
	if recheck {
		if cache, ok := c.ExtendedCache.(interface {
			RetrieveForRecheck(string) (*httpcache.Resource, error)
		}); ok {
			retrieve = cache.RetrieveForRecheck
		}
	}
	res, err := retrieve(key)
	if err != nil {
		return nil, err
	}
	if at, ok := c.staleAtLocked(key); ok && !res.StoredAfter(at) {
		res.MarkStale()
	}
	return res, nil
}
func (c *LockedCache) Store(res *httpcache.Resource, keys ...string) error {
	return c.StoreContext(context.Background(), res, keys...)
}
func (c *LockedCache) StoreContext(ctx context.Context, res *httpcache.Resource, keys ...string) error {
	release, err := c.hold(ctx, keys)
	if err != nil {
		return err
	}
	defer release()
	if err := c.checkReceivedLocked(res, keys); err != nil {
		return err
	}
	if store, ok := c.ExtendedCache.(httpcache.ContextStore); ok {
		return store.StoreContext(ctx, res, keys...)
	}
	return c.ExtendedCache.Store(res, keys...)
}
func (c *LockedCache) Freshen(res *httpcache.Resource, keys ...string) error {
	release, err := c.hold(context.Background(), keys)
	if err != nil {
		return err
	}
	defer release()
	if err := c.checkReceivedLocked(res, keys); err != nil {
		return err
	}
	// The backend may itself invalidate a freshening target whose validators
	// no longer match. Publish only those new markers in the shared journal;
	// its process-local snapshot must never replace another instance's keys.
	before := c.backendStaleSnapshot(keys)
	err = c.ExtendedCache.Freshen(res, keys...)
	for key, at := range c.backendStaleSnapshot(keys) {
		if !at.After(before[key]) {
			continue
		}
		c.invalidateLocked(key, at)
	}
	return err
}
func (c *LockedCache) Invalidate(keys ...string) {
	release, err := c.hold(context.Background(), keys)
	if err != nil {
		for _, key := range keys {
			c.recordFailure(key, failedMarkerTime)
		}
		slog.Error("shared cache invalidation lock failed", "error", err)
		return
	}
	defer release()
	// Choose the timestamp after waiting for older writers. Their persisted
	// store times must precede this barrier even if they completed while this
	// invalidation was waiting for the per-key data lock.
	at := httpcache.Clock()
	for _, key := range keys {
		c.invalidateLocked(key, at)
	}
}
func (c *LockedCache) Cleanup() httpcache.CleanupResult { return httpcache.CleanupResult{} }
func (c *LockedCache) Purge() error {
	return fmt.Errorf("stop all shared-cache instances before repository-wide purge")
}
func (c *LockedCache) StaleAt(key string) (time.Time, bool) {
	release, err := c.hold(context.Background(), []string{key})
	if err != nil {
		return failedMarkerTime, true
	}
	defer release()
	return c.staleAtLocked(key)
}
func (c *LockedCache) StaleSnapshot(keys ...string) map[string]time.Time {
	out := make(map[string]time.Time, len(keys))
	release, err := c.hold(context.Background(), keys)
	if err != nil {
		for _, key := range keys {
			out[key] = failedMarkerTime
		}
		return out
	}
	defer release()
	for _, key := range keys {
		if at, ok := c.staleAtLocked(key); ok {
			out[key] = at
		}
	}
	return out
}

func (c *LockedCache) backendStaleSnapshot(keys []string) map[string]time.Time {
	if cache, ok := c.ExtendedCache.(interface {
		StaleSnapshot(...string) map[string]time.Time
	}); ok {
		return cache.StaleSnapshot(keys...)
	}
	out := make(map[string]time.Time)
	if cache, ok := c.ExtendedCache.(interface {
		StaleAt(string) (time.Time, bool)
	}); ok {
		for _, key := range keys {
			if at, ok := cache.StaleAt(key); ok {
				out[key] = at
			}
		}
	}
	return out
}

func (c *LockedCache) staleAtLocked(key string) (time.Time, bool) {
	at, marked, err := c.Coordinator.readMarker(key)
	if err != nil {
		return failedMarkerTime, true
	}
	// Preserve legacy/backend-local invalidations as well as the independent
	// shared marker. Reading these maps never writes a whole-map snapshot.
	for _, local := range c.backendStaleSnapshot([]string{key}) {
		if !marked || local.After(at) {
			at, marked = local, true
		}
	}
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	if failed, ok := c.failed[key]; ok && (!marked || failed.After(at)) {
		at, marked = failed, true
	}
	return at, marked
}

func (c *LockedCache) checkReceivedLocked(res *httpcache.Resource, keys []string) error {
	for _, key := range keys {
		if at, marked := c.staleAtLocked(key); marked && !res.ReceivedAfter(at) {
			return fmt.Errorf("shared cache response superseded by invalidation at %s", at)
		}
	}
	return nil
}

func (c *LockedCache) recordFailure(key string, at time.Time) {
	c.failedMu.Lock()
	defer c.failedMu.Unlock()
	if c.failed == nil {
		c.failed = make(map[string]time.Time)
	}
	c.failed[key] = at
}

func (c *LockedCache) invalidateLocked(key string, at time.Time) {
	if previous, marked := c.staleAtLocked(key); marked && previous.After(at) {
		at = previous
	}
	if err := c.Coordinator.writeMarker(key, at); err != nil {
		// ExtendedCache.Invalidate has no error result. Keep this process fail
		// closed if persistence fails, and surface the loss of peer durability.
		c.recordFailure(key, failedMarkerTime)
		slog.Error("shared cache invalidation persistence failed", "error", err)
	}
}
