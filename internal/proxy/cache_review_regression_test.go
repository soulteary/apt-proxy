package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitForReviewCache(t *testing.T, count func() int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if count() == 0 {
		t.Fatal("response was not published to cache")
	}
}

func TestZeroFreshnessRequiresUpstreamValidation(t *testing.T) {
	for _, policy := range []string{"max-age=0", "public, max-age=3600, s-maxage=0"} {
		t.Run(policy, func(t *testing.T) {
			var hits atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Cache-Control", policy)
				w.Header().Set("Last-Modified", "Wed, 01 Jan 2020 00:00:00 GMT")
				if r.Header.Get("If-Modified-Since") != "" {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				_, _ = io.WriteString(w, "complete")
			}))
			defer upstream.Close()
			server, cache := downloadTestServer(t, upstream.URL)
			get := func() {
				res, err := server.Client().Get(server.URL + "/ubuntu/dists/jammy/InRelease")
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(res.Body)
				res.Body.Close()
				if err != nil || res.StatusCode != 200 || string(body) != "complete" {
					t.Fatalf("response=%d %q err=%v", res.StatusCode, body, err)
				}
			}
			get()
			waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
			get()
			if hits.Load() < 2 {
				t.Fatal("zero freshness reused without validation")
			}
		})
	}
}

func TestFailedValidationDoesNotServeMustRevalidateEntry(t *testing.T) {
	var unavailable atomic.Bool
	var conditional atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			if r.Header.Get("If-None-Match") != "" {
				conditional.Store(true)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "unhealthy")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, "old-index")
	}))
	defer upstream.Close()
	server, cache := downloadTestServer(t, upstream.URL)
	request := func(directive string) (int, string) {
		req, _ := http.NewRequest("GET", server.URL+"/ubuntu/dists/jammy/InRelease", nil)
		if directive != "" {
			req.Header.Set("Cache-Control", directive)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, string(body)
	}
	if status, _ := request(""); status != 200 {
		t.Fatal(status)
	}
	waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
	unavailable.Store(true)
	status, body := request("min-fresh=7200")
	if !conditional.Load() {
		t.Fatal("request bypassed conditional validation")
	}
	if status != http.StatusServiceUnavailable || body == "old-index" {
		t.Fatalf("failed validation served old object: %d %q", status, body)
	}
}

func TestNotModifiedKeepsDamagedBodyDetectable(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Length", "8")
		_, _ = io.WriteString(w, "complete")
	}))
	defer upstream.Close()
	server, cache, dir := downloadTestServerWithDir(t, upstream.URL)
	get := func(directive string) string {
		req, _ := http.NewRequest("GET", server.URL+"/ubuntu/pool/revalidated.deb", nil)
		if directive != "" {
			req.Header.Set("Cache-Control", directive)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("response=%d %q err=%v", res.StatusCode, body, err)
		}
		return string(body)
	}
	if body := get(""); body != "complete" {
		t.Fatal(body)
	}
	waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
	if body := get("min-fresh=7200"); body != "complete" || hits.Load() != 2 {
		t.Fatalf("304 body=%q hits=%d", body, hits.Load())
	}
	key := "apt-proxy-private-v1:GET:" + upstream.URL + "/ubuntu/pool/revalidated.deb"
	header, err := cache.Header(key)
	if err != nil || header.Get("Content-Length") != "8" {
		t.Fatalf("304 lost stored length: %v %v", header, err)
	}
	var bodyPath string
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.Contains(filepath.ToSlash(path), "/body/v1/") {
			bodyPath = path
		}
		return nil
	})
	if err != nil || bodyPath == "" {
		t.Fatalf("cache body not found: %v", err)
	}
	if err := os.Truncate(bodyPath, 3); err != nil {
		t.Fatal(err)
	}
	if body := get(""); body != "complete" || hits.Load() != 3 {
		t.Fatalf("damaged object survived 304: %q hits=%d", body, hits.Load())
	}
}
