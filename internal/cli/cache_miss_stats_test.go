//go:build unix

package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type repositoryMissStatsMetrics struct {
	httpcache.NopMetrics
	seen   chan struct{}
	misses atomic.Int64
}

func (m *repositoryMissStatsMetrics) RecordCacheMiss(string) {
	m.misses.Add(1)
	select {
	case m.seen <- struct{}{}:
	default:
	}
}

func TestRepositoryCoordinatedRetriesCountOneMissPerClient(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared=%t", shared), func(t *testing.T) {
			const clients = 8
			const payload = "complete uncached repository bytes"
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
			var calls atomic.Int64
			origin := httptest.NewServer(repositoryMissStatsOrigin(&calls, entered, release, payload))
			defer origin.Close()
			defer releaseFirst()
			cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
				Cache: config.CacheConfig{MaxSize: 1, TTL: time.Hour, CleanupInterval: time.Hour, Integrity: "none"},
			})
			cfg.Mirrors.Ubuntu = origin.URL + "/ubuntu/"
			path := "/ubuntu/pool/stats.deb"
			if shared {
				cfg.SharedCache = &sharedcache.Config{CoordinationDir: t.TempDir(), Repositories: []sharedcache.Repository{
					{ID: "public", Bases: []string{cfg.Mirrors.Ubuntu}},
				}}
				path = fmt.Sprintf("/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/%x", sha256.Sum256([]byte(payload)))
			}
			srv, err := NewServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = srv.shutdown() }()
			metrics := &repositoryMissStatsMetrics{seen: make(chan struct{}, clients)}
			srv.cacheProxy.SetMetrics(metrics)
			results := make(chan sharedResult, clients)
			var requests sync.WaitGroup
			defer func() { releaseFirst(); requests.Wait() }()
			for range clients {
				requests.Add(1)
				go func() { defer requests.Done(); results <- getShared(srv, path) }()
			}
			awaitSharedSignal(t, entered, "first repository download did not reach origin")
			for range clients {
				awaitSharedSignal(t, metrics.seen, "not all clients observed the initial cold miss")
			}
			releaseFirst()
			for range clients {
				select {
				case result := <-results:
					if result.err != nil || result.status != 200 || result.body != payload {
						t.Fatalf("coordinated retry lost response: %+v", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("coordinated retry did not complete")
				}
			}
			if err := srv.cacheProxy.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertRepositoryMissStats(t, srv, metrics, &calls, clients)
		})
	}
}

func repositoryMissStatsOrigin(calls *atomic.Int64, entered, release chan struct{}, payload string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = io.WriteString(w, payload)
	}
}

func assertRepositoryMissStats(t *testing.T, srv *Server, metrics *repositoryMissStatsMetrics, calls *atomic.Int64, clients int64) {
	t.Helper()
	if got := calls.Load(); got != clients {
		t.Fatalf("no-store did not exercise repeated fills: %d origin calls, want %d", got, clients)
	}
	statsResult := getShared(srv, "/api/cache/stats")
	if statsResult.err != nil || statsResult.status != 200 {
		t.Fatalf("cache statistics API failed: %+v", statsResult)
	}
	var stats api.CacheStatsResponse
	if err := json.Unmarshal([]byte(statsResult.body), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.MissCount != clients || stats.HitCount != 0 || stats.ItemCount != 0 || stats.HitRate != 0 {
		t.Fatalf("internal retries inflated published client statistics: %+v", stats)
	}
	if got := metrics.misses.Load(); got != clients {
		t.Fatalf("handler metrics counted internal retries: %d misses, want %d", got, clients)
	}
}
