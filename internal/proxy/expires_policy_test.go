package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestRepositoryCachePolicyPreservesExpires(t *testing.T) {
	for _, expires := range []string{
		time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat),
		time.Now().Add(time.Hour).UTC().Format(http.TimeFormat),
		"0", // Invalid dates deliberately express immediate expiration too.
	} {
		for _, status := range []int{http.StatusOK, http.StatusNotFound} {
			header := http.Header{"Expires": []string{expires}}
			applyRepositoryCachePolicy(header, status, &distro.Rule{CacheControl: "max-age=3600"})
			if header.Get("Cache-Control") != "" || header.Get("Expires") != expires {
				t.Fatalf("explicit expiration replaced: status=%d headers=%v", status, header)
			}
		}
	}
}

func TestRepositoryExpiresControlsActualCacheFreshness(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expires string
		fresh   bool
	}{
		{"expired", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), false},
		{"future", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), true},
		{"invalid", "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.Header().Set("Expires", tc.expires)
				_, _ = io.WriteString(w, "repository metadata")
			}))
			defer upstream.Close()
			server, cache := downloadTestServer(t, upstream.URL)
			get := func() {
				t.Helper()
				res, err := server.Client().Get(server.URL + "/ubuntu/dists/jammy/InRelease")
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(res.Body)
				res.Body.Close()
				if err != nil || res.StatusCode != 200 || string(body) != "repository metadata" {
					t.Fatalf("response: status=%d body=%q err=%v", res.StatusCode, body, err)
				}
				if res.Header.Get("Expires") != tc.expires || res.Header.Get("Cache-Control") != "" {
					t.Fatalf("local defaults changed upstream expiration: %v", res.Header)
				}
			}
			get()
			if tc.fresh {
				deadline := time.Now().Add(2 * time.Second)
				for cache.Stats().ItemCount == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if cache.Stats().ItemCount == 0 {
					t.Fatal("fresh response did not reach storage")
				}
			}
			get()
			want := int64(2)
			if tc.fresh {
				want = 1
			}
			if hits.Load() != want {
				t.Fatalf("Expires freshness ignored: upstream hits=%d want=%d", hits.Load(), want)
			}
		})
	}
}
