package httpcache_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/lox/httpcache"
)

func BenchmarkCachingFiles(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "max-age=100000")
		fmt.Fprintf(w, "cache server payload")
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
		resp.Body.Close()
	}
}
