package httpcache_test

import (
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/soulteary/apt-proxy/pkg/httpcache"
)

func TestSaveResource(t *testing.T) {
	var body = strings.Repeat("llamas", 5000)
	var cache = httpcache.NewMemoryCache()

	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
		"Llamas": []string{"true"},
	})

	if err := cache.Store(res, "testkey"); err != nil {
		t.Fatal(err)
	}

	resOut, err := cache.Retrieve("testkey")
	if err != nil {
		t.Fatal(err)
	}

	if resOut == nil {
		t.Fatalf("resOut should not be null")
	}

	if !reflect.DeepEqual(res.Header(), resOut.Header()) {
		t.Fatalf("header should be equal")
	}

	if body != readAllString(resOut) {
		t.Fatalf("body should be equal")
	}
}

func TestSaveResourceWithIncorrectContentLength(t *testing.T) {
	var body = "llamas"
	var cache = httpcache.NewMemoryCache()

	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
		"Llamas":         []string{"true"},
		"Content-Length": []string{"10"},
	})

	if err := cache.Store(res, "testkey"); err == nil {
		t.Fatal("Entry should have generated an error")
	}

	_, err := cache.Retrieve("testkey")
	if err != httpcache.ErrNotFoundInCache {
		t.Fatal("Entry shouldn't have been cached")
	}
}

// TestStoreWithMultipleKeys ensures Store writes the same body for every key (e.g. primary + Vary key).
// Regression test for the bug where the first key consumed the body reader and later keys got empty body.
func TestStoreWithMultipleKeys(t *testing.T) {
	body := strings.Repeat("vary-body", 500)
	cache := httpcache.NewMemoryCache()

	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
		"Vary": []string{"Accept-Encoding"},
	})

	keyPrimary := "GET:https://example.com/path"
	keyVary := "GET:https://example.com/path::Accept-Encoding=gzip:"
	if err := cache.Store(res, keyPrimary, keyVary); err != nil {
		t.Fatalf("Store(...): %v", err)
	}

	for _, key := range []string{keyPrimary, keyVary} {
		resOut, err := cache.Retrieve(key)
		if err != nil {
			t.Fatalf("Retrieve(%q): %v", key, err)
		}
		got := readAllString(resOut)
		if got != body {
			t.Fatalf("key %q: body len=%d want len=%d; body mismatch", key, len(got), len(body))
		}
		_ = resOut.Close()
	}
}

// TestCloseIdempotent asserts that calling Close() multiple times (including concurrently) does not panic.
func TestCloseIdempotent(t *testing.T) {
	config := httpcache.DefaultCacheConfig().WithCleanupInterval(0)
	ext := httpcache.NewMemoryCacheWithConfig(config)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ext.Close()
		}()
	}
	wg.Wait()
	// Additional sequential Close calls
	_ = ext.Close()
	_ = ext.Close()
}
