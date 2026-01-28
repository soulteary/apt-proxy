package httpcache_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/pkg/httpcache"
)

func BenchmarkCachingFiles(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "max-age=100000")
		_, _ = fmt.Fprintf(w, "cache server payload")
	}))
	defer backend.Close()

	u, err := url.Parse(backend.URL)
	if err != nil {
		b.Fatal(err)
	}

	handler := httpcache.NewHandler(httpcache.NewMemoryCache(), httputil.NewSingleHostReverseProxy(u))
	handler.Shared = true
	cacheServer := httptest.NewServer(handler)
	defer cacheServer.Close()

	for n := 0; n < b.N; n++ {
		client := http.Client{}
		resp, err := client.Get(fmt.Sprintf("%s/llamas/%d", cacheServer.URL, n))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp.Body.Close()
	}
}

// BenchmarkCacheLookup benchmarks cache key lookup performance (Key.String() and hashKey)
func BenchmarkCacheLookup(b *testing.B) {
	testURLs := []string{
		"https://example.com/path",
		"https://example.com/very/long/path/to/resource",
		"https://example.com/path?query=value&other=param",
		"https://example.com/path?very=long&query=string&with=many&parameters=here",
	}

	cache := httpcache.NewMemoryCache()
	keys := make([]string, len(testURLs))

	// Pre-populate cache with test data
	for i, testURL := range testURLs {
		u, _ := url.Parse(testURL)
		key := httpcache.NewKey("GET", u, http.Header{})
		keys[i] = key.String()

		body := strings.Repeat("test-body", 100)
		res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
			"Cache-Control": []string{"max-age=3600"},
		})
		if err := cache.Store(res, keys[i]); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		idx := n % len(keys)
		_, err := cache.Retrieve(keys[idx])
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCacheLookupWithVary benchmarks cache lookup with Vary headers
func BenchmarkCacheLookupWithVary(b *testing.B) {
	testURL := "https://example.com/path"
	u, _ := url.Parse(testURL)
	req := &http.Request{
		Method: "GET",
		URL:    u,
		Header: http.Header{
			"Accept-Encoding": []string{"gzip"},
		},
	}

	key := httpcache.NewRequestKey(req)
	varyKey := key.Vary("Accept-Encoding", req)

	cache := httpcache.NewMemoryCache()
	body := strings.Repeat("test-body", 100)
	res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
		"Vary":           []string{"Accept-Encoding"},
		"Cache-Control":  []string{"max-age=3600"},
		"Content-Length": []string{fmt.Sprintf("%d", len(body))},
	})

	keyStr := key.String()
	varyKeyStr := varyKey.String()
	if err := cache.Store(res, keyStr, varyKeyStr); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		// Alternate between primary and vary key
		var testKey string
		if n%2 == 0 {
			testKey = keyStr
		} else {
			testKey = varyKeyStr
		}
		_, err := cache.Retrieve(testKey)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCacheRetrieve benchmarks cache retrieve performance for different sizes
func BenchmarkCacheRetrieve(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"Small", 1 * 1024},         // 1KB
		{"Medium", 100 * 1024},      // 100KB
		{"Large", 10 * 1024 * 1024}, // 10MB
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			cache := httpcache.NewMemoryCache()
			body := strings.Repeat("x", size.size)
			res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
				"Cache-Control":  []string{"max-age=3600"},
				"Content-Length": []string{fmt.Sprintf("%d", len(body))},
			})

			key := "test-key-" + size.name
			if err := cache.Store(res, key); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for n := 0; n < b.N; n++ {
				resOut, err := cache.Retrieve(key)
				if err != nil {
					b.Fatal(err)
				}
				// Read the body to simulate actual usage
				buf := make([]byte, 32*1024)
				for {
					_, err := resOut.Read(buf)
					if err != nil {
						break
					}
				}
				_ = resOut.Close()
			}
		})
	}
}

// BenchmarkStoreMultipleKeys benchmarks storing resources with multiple keys (Vary scenario)
func BenchmarkStoreMultipleKeys(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"Small", 1 * 1024},    // 1KB
		{"Medium", 100 * 1024}, // 100KB
		{"Large", 1024 * 1024}, // 1MB
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			cache := httpcache.NewMemoryCache()
			body := strings.Repeat("test-body", size.size/9) // Approximate size
			if len(body) < size.size {
				body += strings.Repeat("x", size.size-len(body))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for n := 0; n < b.N; n++ {
				res := httpcache.NewResourceBytes(http.StatusOK, []byte(body), http.Header{
					"Vary":           []string{"Accept-Encoding"},
					"Cache-Control":  []string{"max-age=3600"},
					"Content-Length": []string{fmt.Sprintf("%d", len(body))},
				})

				keyPrimary := fmt.Sprintf("GET:https://example.com/path-%d", n)
				keyVary := fmt.Sprintf("GET:https://example.com/path-%d::Accept-Encoding=gzip:", n)

				if err := cache.Store(res, keyPrimary, keyVary); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
