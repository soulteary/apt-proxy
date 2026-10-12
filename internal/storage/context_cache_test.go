package storage

import (
	"context"
	"errors"
	"slices"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type ownershipContextKey struct{}

type legacyContextFixture struct {
	httpcache.ExtendedCache
	resource *httpcache.Resource
	keys     []string
	calls    int
}

func (c *legacyContextFixture) Store(res *httpcache.Resource, keys ...string) error {
	c.resource, c.keys = res, slices.Clone(keys)
	c.calls++
	return nil
}

type ownershipContextFixture struct {
	legacyContextFixture
	ctx context.Context
}

func (c *ownershipContextFixture) StoreContext(ctx context.Context, res *httpcache.Resource, keys ...string) error {
	c.ctx = ctx
	return c.Store(res, keys...)
}

func TestVerifiedCacheStoreContextPreservesOwnershipAndNamespace(t *testing.T) {
	backend := &ownershipContextFixture{}
	cache := &VerifiedCache{ExtendedCache: backend}
	ctx := context.WithValue(context.Background(), ownershipContextKey{}, "leader-token")
	res := httpcache.NewResourceBytes(200, []byte("complete"), nil)
	defer res.Close()
	if err := cache.StoreContext(ctx, res, "GET:http://mirror.example/package.deb", "vary-key"); err != nil {
		t.Fatal(err)
	}
	if backend.ctx.Value(ownershipContextKey{}) != "leader-token" || backend.resource != res || backend.calls != 1 {
		t.Fatal("cache wrapper stripped publication ownership or resource")
	}
	want := []string{"apt-proxy-private-v1:GET:http://mirror.example/package.deb", "apt-proxy-private-v1:vary-key"}
	if !slices.Equal(backend.keys, want) {
		t.Fatalf("context publication escaped the private cache namespace: %v", backend.keys)
	}
}

func TestVerifiedCacheStoreContextLegacyFallbackHonorsCancellation(t *testing.T) {
	backend := &legacyContextFixture{}
	cache := &VerifiedCache{ExtendedCache: backend}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.StoreContext(ctx, nil, "key"); !errors.Is(err, context.Canceled) || backend.calls != 0 {
		t.Fatalf("canceled publication reached legacy backend: calls=%d err=%v", backend.calls, err)
	}
	res := httpcache.NewResourceBytes(200, []byte("complete"), nil)
	defer res.Close()
	if err := cache.StoreContext(context.Background(), res, "key"); err != nil {
		t.Fatal(err)
	}
	if backend.resource != res || backend.calls != 1 || !slices.Equal(backend.keys, []string{"apt-proxy-private-v1:key"}) {
		t.Fatal("legacy fallback lost resource or namespace")
	}
}
