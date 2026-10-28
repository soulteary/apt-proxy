package storage

import (
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type namespaceCache struct {
	httpcache.ExtendedCache
	keys []string
}

func (c *namespaceCache) Header(key string) (httpcache.Header, error) {
	c.keys = []string{key}
	return httpcache.Header{}, nil
}
func (c *namespaceCache) Freshen(_ *httpcache.Resource, keys ...string) error {
	c.keys = keys
	return nil
}
func (c *namespaceCache) StaleAt(key string) (time.Time, bool) {
	c.keys = []string{key}
	return time.Unix(42, 0), true
}

func TestVerifiedCacheNamespacesMetadataAndLegacyMarkers(t *testing.T) {
	b := &namespaceCache{}
	c := &VerifiedCache{ExtendedCache: b}
	if _, err := c.Header("key"); err != nil || b.keys[0] != privateCacheNamespace+"key" {
		t.Fatal("header namespace lost")
	}
	if err := c.Freshen(nil, "key", "variant"); err != nil || b.keys[1] != privateCacheNamespace+"variant" {
		t.Fatal("freshen namespace lost")
	}
	if got := c.StaleSnapshot("key"); !got["key"].Equal(time.Unix(42, 0)) || b.keys[0] != privateCacheNamespace+"key" {
		t.Fatal("legacy marker translation lost")
	}
	c.ExtendedCache = &struct{ httpcache.ExtendedCache }{}
	if _, ok := c.StaleAt("key"); ok {
		t.Fatal("unsupported backend marked stale")
	}
	if len(c.StaleSnapshot("key")) != 0 {
		t.Fatal("unsupported backend invented snapshot")
	}
}

func TestVerifiedCacheRejectsInvalidDeclaredLength(t *testing.T) {
	for _, length := range []string{"invalid", "-1"} {
		res := httpcache.NewResourceBytes(200, []byte("data"), http.Header{"Content-Length": {length}})
		c := &VerifiedCache{ExtendedCache: &fixtureCache{resource: res}}
		if got, err := c.Retrieve("GET:http://example/pkg"); err != httpcache.ErrNotFoundInCache || got != nil {
			t.Fatalf("invalid length %q accepted", length)
		}
	}
}

type brokenSeekBody struct {
	failWhence int
	closed     bool
}

func (b *brokenSeekBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *brokenSeekBody) Seek(_ int64, whence int) (int64, error) {
	if whence == b.failWhence {
		return 0, errBrokenSeek
	}
	if whence == io.SeekEnd {
		return 4, nil
	}
	return 0, nil
}
func (b *brokenSeekBody) Close() error { b.closed = true; return nil }

var errBrokenSeek = errors.New("seek failed")

func TestVerifiedCacheClosesUnreadableSeekBodies(t *testing.T) {
	for _, whence := range []int{io.SeekEnd, io.SeekStart} {
		body := &brokenSeekBody{failWhence: whence}
		res := httpcache.NewResource(200, body, http.Header{"Content-Length": {"4"}})
		cache := &VerifiedCache{ExtendedCache: &fixtureCache{resource: res}}
		if got, err := cache.Retrieve("GET:http://example/pkg"); !errors.Is(err, errBrokenSeek) || got != nil || !body.closed {
			t.Fatal("broken seek body returned or leaked")
		}
	}
}
