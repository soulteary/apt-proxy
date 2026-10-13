package storage

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestVerifiedCacheVersionedVaryRejectsDamagedDiskBodies(t *testing.T) {
	for _, retrieval := range []string{"lookup", "recheck"} {
		for _, damage := range []string{"truncate", "append"} {
			t.Run(retrieval+"/"+damage, func(t *testing.T) {
				assertVersionedVaryBodyValidation(t, retrieval, damage)
			})
		}
	}
}

func assertVersionedVaryBodyValidation(t *testing.T, retrieval, damage string) {
	t.Helper()
	dir := t.TempDir()
	backend, err := httpcache.NewDiskCacheWithConfig(dir, &httpcache.CacheConfig{Integrity: httpcache.IntegrityDisabled})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	cache := &VerifiedCache{ExtendedCache: backend}
	req := httptest.NewRequest(http.MethodGet, "http://mirror.example/package.deb", nil)
	req.Header.Set("Accept-Language", "en")
	key := httpcache.NewRequestKey(req).Vary("Accept-Language", req).String()
	if strings.HasPrefix(key, "GET:") {
		t.Fatal("dependency did not use a versioned Vary namespace")
	}
	storeVersionedVaryBody(t, cache, key)
	damageVersionedVaryBody(t, dir, damage)
	retrieve := cache.Retrieve
	if retrieval == "recheck" {
		retrieve = cache.RetrieveForRecheck
	}
	res, err := retrieve(key)
	if res != nil {
		_ = res.Close()
	}
	if !errors.Is(err, httpcache.ErrNotFoundInCache) {
		t.Fatalf("damaged versioned Vary resource accepted: %v", err)
	}
	storeVersionedVaryBody(t, cache, key)
	res, err = retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res)
	_ = res.Close()
	if readErr != nil || string(body) != "complete" {
		t.Fatalf("repaired versioned entry: %q %v", body, readErr)
	}
}

func storeVersionedVaryBody(t *testing.T, cache *VerifiedCache, key string) {
	t.Helper()
	res := httpcache.NewResourceBytes(http.StatusOK, []byte("complete"), http.Header{"Content-Length": {"8"}, "Cache-Control": {"max-age=3600"}, "Vary": {"Accept-Language"}})
	defer func() { _ = res.Close() }()
	if err := cache.Store(res, key); err != nil {
		t.Fatal(err)
	}
}

func damageVersionedVaryBody(t *testing.T, dir, damage string) {
	t.Helper()
	var bodies []string
	err := filepath.WalkDir(filepath.Join(dir, "body"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			bodies = append(bodies, path)
		}
		return nil
	})
	if err != nil || len(bodies) != 1 {
		t.Fatalf("body paths: %v %v", bodies, err)
	}
	if damage == "truncate" {
		if err := os.Truncate(bodies[0], 3); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.OpenFile(bodies[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("extra")
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append damage: %v %v", err, closeErr)
	}
}

func TestVerifiedCacheVersionedVaryPreservesHeadMetadata(t *testing.T) {
	for _, retrieval := range []string{"lookup", "recheck"} {
		t.Run(retrieval, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodHead, "http://mirror.example/package.deb", nil)
			req.Header.Set("Accept-Language", "en")
			key := httpcache.NewRequestKey(req).Vary("Accept-Language", req).String()
			res := httpcache.NewResourceBytes(http.StatusOK, nil, http.Header{"Content-Length": {"8"}})
			cache := &VerifiedCache{ExtendedCache: &fixtureCache{resource: res}}
			retrieve := cache.Retrieve
			if retrieval == "recheck" {
				retrieve = cache.RetrieveForRecheck
			}
			got, err := retrieve(key)
			if err != nil || got != res {
				t.Fatalf("valid varied HEAD metadata rejected: %v", err)
			}
			_ = got.Close()
		})
	}
}
