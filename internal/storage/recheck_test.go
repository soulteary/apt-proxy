package storage

import (
	"errors"
	"io"
	"net/http"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestVerifiedRecheckPreservesPrivateNamespaceAndValidation(t *testing.T) {
	backend := httpcache.NewMemoryCacheWithConfig(httpcache.DefaultCacheConfig().WithCleanupInterval(0))
	defer func() { _ = backend.Close() }()
	cache := &VerifiedCache{ExtendedCache: backend}
	const key = "GET:http://origin.test/package.deb"
	if err := backend.Store(httpcache.NewResourceBytes(200, []byte("legacy-session"), http.Header{}), key); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := cache.RetrieveForRecheck(key); !errors.Is(err, httpcache.ErrNotFoundInCache) {
			t.Fatalf("legacy namespace became reachable through recheck: %v", err)
		}
	}
	if got := backend.Stats().MissCount; got != 0 {
		t.Fatalf("private namespace rechecks counted misses: %d", got)
	}
	for _, tc := range []struct {
		name   string
		header http.Header
		valid  bool
	}{
		{"valid", http.Header{"Content-Length": {"8"}}, true},
		{"bad-length", http.Header{"Content-Length": {"80"}}, false},
		{"legacy-session", http.Header{"Content-Length": {"8"}, "Set-Cookie": {"session=legacy"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertVerifiedRecheckMetadata(t, cache, backend, key, tc.header, tc.valid)
		})
	}
}

func assertVerifiedRecheckMetadata(t *testing.T, cache *VerifiedCache, backend interface{ Stats() httpcache.CacheStats }, key string, header http.Header, valid bool) {
	t.Helper()
	if !valid {
		// A legacy/custom backend can expose malformed metadata that
		// current native Store correctly refuses to create itself.
		legacy := &VerifiedCache{ExtendedCache: &fixtureCache{resource: httpcache.NewResourceBytes(200, []byte("verified"), header)}}
		res, err := legacy.RetrieveForRecheck(key)
		if !errors.Is(err, httpcache.ErrNotFoundInCache) || res != nil {
			t.Fatalf("recheck bypassed verification: resource=%v err=%v", res, err)
		}
		return
	}
	if err := cache.Store(httpcache.NewResourceBytes(200, []byte("verified"), header), key); err != nil {
		t.Fatal(err)
	}
	res, err := cache.RetrieveForRecheck(key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res)
	_ = res.Close()
	if err != nil || string(body) != "verified" {
		t.Fatalf("recheck reached wrong namespace/body: %q %v", body, err)
	}
	if stats := backend.Stats(); stats.HitCount != 0 || stats.MissCount != 0 {
		t.Fatalf("verified recheck counted another public lookup: %+v", stats)
	}
	cache.Invalidate(key)
	res, err = cache.RetrieveForRecheck(key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Close() }()
	if !res.IsStale() {
		t.Fatal("recheck lost native invalidation barrier")
	}
}

func TestVerifiedRecheckKeepsLegacyBackendFallback(t *testing.T) {
	cache := &VerifiedCache{ExtendedCache: &fixtureCache{resource: httpcache.NewResourceBytes(200, []byte("fallback"), http.Header{})}}
	res, err := cache.RetrieveForRecheck("GET:http://origin.test/fallback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Close() }()
	body, err := io.ReadAll(res)
	if err != nil || string(body) != "fallback" {
		t.Fatalf("legacy backend was no longer compatible: %q %v", body, err)
	}
}
