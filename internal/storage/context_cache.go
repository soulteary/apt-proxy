package storage

import (
	"context"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// StoreContext retains ownership/fencing values while applying the same private
// namespace as Store. Without this optional-interface forwarding, wrapping the
// cache would silently strip coordinator context at publication.
func (c *VerifiedCache) StoreContext(ctx context.Context, res *httpcache.Resource, keys ...string) error {
	if backend, ok := c.ExtendedCache.(httpcache.ContextStore); ok {
		return backend.StoreContext(ctx, res, privateCacheKeys(keys)...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.ExtendedCache.Store(res, privateCacheKeys(keys)...)
}

var _ httpcache.ContextStore = (*VerifiedCache)(nil)
