package proxy

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestRepositoryRangeCache(t *testing.T) {
	var hits atomic.Int64
	const body = "0123456789"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("ETag", `"package-v1"`)
		http.ServeContent(w, r, "package.deb", time.Time{}, bytes.NewReader([]byte(body)))
	}))
	defer upstream.Close()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default()})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := httpcache.NewHandlerWithOptions(cache, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	get := func(path, rangeHeader, ifRange string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://proxy.example/ubuntu/pool/"+path, nil)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		if ifRange != "" {
			req.Header.Set("If-Range", ifRange)
		}
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)
		return rec
	}
	// A cold partial response must never become the complete-object cache entry.
	rec := get("cold.deb", "bytes=2-5", "")
	if rec.Code != 206 || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("cold range: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	rec = get("cold.deb", "", "")
	if rec.Code != 200 || rec.Body.String() != body {
		t.Fatalf("partial response poisoned full request: %d %q", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for cache.Stats().ItemCount == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if cache.Stats().ItemCount == 0 {
		t.Fatal("complete package not stored")
	}
	before := hits.Load()
	for _, tc := range []struct {
		rangeHeader        string
		status             int
		body, contentRange string
	}{
		{"bytes=0-3", 206, "0123", "bytes 0-3/10"},
		{"bytes=7-", 206, "789", "bytes 7-9/10"},
		{"bytes=-3", 206, "789", "bytes 7-9/10"},
		{"bytes=20-", 416, "", "bytes */10"},
	} {
		rec = get("cold.deb", tc.rangeHeader, "")
		if rec.Code != tc.status || (tc.status == 206 && rec.Body.String() != tc.body) || rec.Header().Get("Content-Range") != tc.contentRange {
			t.Fatalf("warm %s: %d %q %v", tc.rangeHeader, rec.Code, rec.Body.String(), rec.Header())
		}
	}
	if hits.Load() != before {
		t.Fatal("warm ranges unexpectedly downloaded upstream")
	}
	rec = get("cold.deb", "bytes=5-", `"package-v1"`)
	if rec.Code != 206 || rec.Body.String() != "56789" {
		t.Fatalf("If-Range resume: %d %q", rec.Code, rec.Body.String())
	}
	rec = get("cold.deb", "bytes=5-", `"old-package"`)
	if rec.Code != 200 || rec.Body.String() != body {
		t.Fatalf("changed If-Range: %d %q", rec.Code, rec.Body.String())
	}
	if hits.Load() != before+2 {
		t.Fatal("If-Range was not validated upstream")
	}
}
