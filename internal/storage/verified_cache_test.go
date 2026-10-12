package storage

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestVerifiedCacheRejectsDamagedDiskBodies(t *testing.T) {
	for _, damage := range []string{"truncate", "append"} {
		t.Run(damage, func(t *testing.T) {
			dir := t.TempDir()
			backend, err := httpcache.NewDiskCacheWithConfig(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			cache := &VerifiedCache{ExtendedCache: backend}
			const key = "GET:http://mirror.example/package.deb"
			store := func() {
				t.Helper()
				res := httpcache.NewResourceBytes(200, []byte("complete"), http.Header{"Content-Length": []string{"8"}, "Cache-Control": []string{"max-age=3600"}})
				if err := cache.Store(res, key); err != nil {
					t.Fatal(err)
				}
			}
			store()
			var bodyPath string
			err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasPrefix(path, filepath.Join(dir, "body")+string(os.PathSeparator)) {
					bodyPath = path
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if bodyPath == "" {
				t.Fatal("body file not found")
			}
			if damage == "truncate" {
				err = os.Truncate(bodyPath, 3)
			} else {
				var f *os.File
				f, err = os.OpenFile(bodyPath, os.O_APPEND|os.O_WRONLY, 0)
				if err == nil {
					_, err = f.WriteString("extra")
					f.Close()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if res, err := cache.Retrieve(key); err != httpcache.ErrNotFoundInCache {
				if res != nil {
					res.Close()
				}
				t.Fatalf("damaged resource returned: %v", err)
			}
			store()
			res, err := cache.Retrieve(key)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Close()
			body, err := io.ReadAll(res)
			if err != nil || string(body) != "complete" {
				t.Fatalf("repair failed: %q %v", body, err)
			}
		})
	}
}

func TestVerifiedCachePreservesInvalidationMarkers(t *testing.T) {
	backend, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	cache := &VerifiedCache{ExtendedCache: backend}
	cache.Invalidate("GET:http://mirror.example/package.deb")
	at, marked := cache.StaleAt("GET:http://mirror.example/package.deb")
	if !marked || at.IsZero() {
		t.Fatal("lost invalidation marker")
	}
	if snapshot := cache.StaleSnapshot("GET:http://mirror.example/package.deb"); !snapshot["GET:http://mirror.example/package.deb"].Equal(at) {
		t.Fatal("lost marker snapshot")
	}
}

type fixtureCache struct {
	httpcache.ExtendedCache
	resource *httpcache.Resource
}

func (c *fixtureCache) Retrieve(string) (*httpcache.Resource, error) { return c.resource, nil }

func TestVerifiedCacheAllowsHeadAndUnknownLength(t *testing.T) {
	for _, tc := range []struct {
		key    string
		header http.Header
	}{
		{"HEAD:http://mirror.example/package.deb", http.Header{"Content-Length": []string{"8"}}},
		{"GET:http://mirror.example/package.deb", make(http.Header)},
	} {
		resource := httpcache.NewResourceBytes(200, nil, tc.header)
		cache := &VerifiedCache{ExtendedCache: &fixtureCache{resource: resource}}
		got, err := cache.Retrieve(tc.key)
		if err != nil || got != resource {
			t.Fatalf("valid metadata rejected: %s %v", tc.key, err)
		}
		got.Close()
	}
}

func TestVerifiedCacheRejectsLegacySessionResponse(t *testing.T) {
	resource := httpcache.NewResourceBytes(200, []byte("session"), http.Header{"Set-Cookie": []string{"session=legacy"}, "Cache-Control": []string{"public, max-age=3600"}})
	cache := &VerifiedCache{ExtendedCache: &fixtureCache{resource: resource}}
	if got, err := cache.Retrieve("GET:http://mirror.example/package.deb"); err != httpcache.ErrNotFoundInCache {
		if got != nil {
			got.Close()
		}
		t.Fatalf("legacy session response accepted: %v", err)
	}
}

func TestPrivateCacheNamespaceExcludesLegacyObjects(t *testing.T) {
	backend, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	const key = "GET:http://mirror.example/package.deb"
	header := http.Header{"Cache-Control": []string{"public, max-age=3600"}}
	if err := backend.Store(httpcache.NewResourceBytes(200, []byte("legacy-authenticated-body"), header.Clone()), key); err != nil {
		t.Fatal(err)
	}
	cache := &VerifiedCache{ExtendedCache: backend}
	if res, err := cache.Retrieve(key); err != httpcache.ErrNotFoundInCache {
		if res != nil {
			res.Close()
		}
		t.Fatalf("legacy body reachable: %v", err)
	}
	if err := cache.Store(httpcache.NewResourceBytes(200, []byte("new-anonymous-body"), header.Clone()), key); err != nil {
		t.Fatal(err)
	}
	res, err := cache.Retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res)
	res.Close()
	if err != nil || string(body) != "new-anonymous-body" {
		t.Fatal("new namespace did not serve new content")
	}
	legacy, err := backend.Retrieve(key)
	if err != nil {
		t.Fatal("migration destructively removed legacy body")
	}
	legacy.Close()
	cache.Invalidate(key)
	res, err = cache.Retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if !res.IsStale() {
		t.Fatal("namespace lost invalidation ordering")
	}
}
