// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cli

// server_isolation_test.go exercises the "two Servers in the same
// process never observe each other's writes" invariant that became
// reachable once the package-level globals were torn out (see
// internal/state/app.go's package doc). The tests here intentionally
// avoid Server.Start so they don't touch the global SIGTERM/SIGHUP
// signal handlers; we drive the Fiber app via app.Test instead.
//
// The integration-tagged sibling at tests/integration/multi_server_test.go
// covers the same invariants on real listening ports.

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// makeIsolationServer builds a Server that's safe to use from
// isolation tests: each call gets its own cache dir, mirror prefix,
// and API key. We deliberately reuse withTestMirrors *only* when no
// explicit mirrors are passed in (so the Mirrors override below wins).
func makeIsolationServer(t *testing.T, name, apiKey, mirrorPrefix string) *Server {
	t.Helper()
	cacheDir := t.TempDir()
	cfg := &config.Config{
		Debug:    false,
		CacheDir: cacheDir,
		Mode:     distro.TypeAllDistros,
		Listen:   "127.0.0.1:0",
		Mirrors: config.MirrorConfig{
			Ubuntu:      mirrorPrefix + "/ubuntu/",
			UbuntuPorts: mirrorPrefix + "/ubuntu-ports/",
			Debian:      mirrorPrefix + "/debian/",
			CentOS:      mirrorPrefix + "/centos/",
			Alpine:      mirrorPrefix + "/alpine/",
		},
		Security: config.SecurityConfig{
			EnableAPIAuth: apiKey != "",
			APIKey:        apiKey,
			// Disable rate limit so the parallel goroutines below
			// don't end up bouncing off the per-IP bucket.
			APIRateLimitPerMinute: 0,
		},
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer(%s): %v", name, err)
	}
	t.Cleanup(func() {
		// shutdown() closes the cache and tracing; safe even though we
		// never called Start.
		_ = srv.shutdown()
	})
	return srv
}

// TestTwoServersStateIsolation hammers SetMirror/GetMirror on two
// Server instances in parallel and asserts neither one sees the other's
// writes. With package-level state this would have been a guaranteed
// race; with per-Server AppState it must be quiet under -race.
func TestTwoServersStateIsolation(t *testing.T) {
	srvA := makeIsolationServer(t, "A", "keyA", "http://serverA.example.com")
	srvB := makeIsolationServer(t, "B", "keyB", "http://serverB.example.com")

	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			srvA.state.SetMirror(distro.TypeUbuntu, "http://serverA.example.com/ubuntu/")
			if got := srvA.state.GetMirror(distro.TypeUbuntu); got == nil ||
				got.String() != "http://serverA.example.com/ubuntu/" {
				t.Errorf("server A leaked: %v", got)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			srvB.state.SetMirror(distro.TypeUbuntu, "http://serverB.example.com/ubuntu/")
			if got := srvB.state.GetMirror(distro.TypeUbuntu); got == nil ||
				got.String() != "http://serverB.example.com/ubuntu/" {
				t.Errorf("server B leaked: %v", got)
				return
			}
		}
	}()
	wg.Wait()

	// Final pointer identity check: each Server's state object should
	// still be a different *AppState. (Sanity assertion against an
	// accidental shared global creeping back in via Server.initialize.)
	if srvA.state == srvB.state {
		t.Fatal("expected per-Server AppState pointers to differ")
	}
}

// TestTwoServersAPIKeyIsolation verifies that a key valid against one
// server is rejected by the other. A regression here would mean the
// auth middleware accidentally consulted some shared global key.
func TestTwoServersAPIKeyIsolation(t *testing.T) {
	srvA := makeIsolationServer(t, "A", "keyA", "http://serverA.example.com")
	srvB := makeIsolationServer(t, "B", "keyB", "http://serverB.example.com")

	cases := []struct {
		name       string
		srv        *Server
		key        string
		wantStatus int
	}{
		{"A accepts keyA", srvA, "keyA", http.StatusOK},
		{"A rejects keyB", srvA, "keyB", http.StatusUnauthorized},
		{"B accepts keyB", srvB, "keyB", http.StatusOK},
		{"B rejects keyA", srvB, "keyA", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/api/cache/stats", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Host = "localhost"
			req.Header.Set("X-API-Key", tc.key)

			resp, err := tc.srv.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("status = %d, want %d (body=%s)", resp.StatusCode, tc.wantStatus, body)
			}
		})
	}
}

// TestTwoServersCacheStatsIsolation purges A and inspects B; they
// should have independent statistics views (different cache backends,
// different on-disk dirs).
func TestTwoServersCacheStatsIsolation(t *testing.T) {
	srvA := makeIsolationServer(t, "A", "keyA", "http://serverA.example.com")
	srvB := makeIsolationServer(t, "B", "keyB", "http://serverB.example.com")

	if srvA.cache == srvB.cache {
		t.Fatal("expected per-Server cache instances to differ")
	}
	if srvA.config.CacheDir == srvB.config.CacheDir {
		t.Fatalf("expected per-Server cache dirs to differ; got %q", srvA.config.CacheDir)
	}

	// Purge A; B's stats endpoint should still respond independently.
	purge, _ := http.NewRequest(http.MethodPost, "/api/cache/purge", nil)
	purge.Host = "localhost"
	purge.Header.Set("X-API-Key", "keyA")
	resp, err := srvA.app.Test(purge)
	if err != nil {
		t.Fatalf("purge A: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge A status = %d", resp.StatusCode)
	}

	stats, _ := http.NewRequest(http.MethodGet, "/api/cache/stats", nil)
	stats.Host = "localhost"
	stats.Header.Set("X-API-Key", "keyB")
	resp, err = srvB.app.Test(stats)
	if err != nil {
		t.Fatalf("stats B: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats B status = %d", resp.StatusCode)
	}
	var dec api.CacheStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&dec); err != nil {
		t.Fatalf("decode B stats: %v", err)
	}
	// Items count is implementation-dependent for a fresh cache, but
	// the call must succeed (i.e. B isn't talking to A's cache).
}

// TestTwoServersRefreshMirrorsParallel exercises the refresh path on
// two Servers simultaneously. The proxy package serializes refresh
// per-Server (refreshMu) but the two Servers must not contend on each
// other; this asserts the cross-Server claim and is also a -race
// regression anchor.
func TestTwoServersRefreshMirrorsParallel(t *testing.T) {
	srvA := makeIsolationServer(t, "A", "", "http://serverA.example.com")
	srvB := makeIsolationServer(t, "B", "", "http://serverB.example.com")

	const iterations = 20
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			srvA.proxy.RefreshMirrors()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			srvB.proxy.RefreshMirrors()
		}
	}()
	wg.Wait()

	// After all refreshes, each server's Ubuntu mirror should still be
	// pointing at its own configured prefix. This is the user-visible
	// guarantee: even though refresh churns rewriters, ownership of
	// mirrors stays per-Server.
	if got := srvA.state.GetMirror(distro.TypeUbuntu); got == nil ||
		got.String() != "http://serverA.example.com/ubuntu/" {
		t.Errorf("server A ubuntu mirror after refresh = %v, want serverA prefix", got)
	}
	if got := srvB.state.GetMirror(distro.TypeUbuntu); got == nil ||
		got.String() != "http://serverB.example.com/ubuntu/" {
		t.Errorf("server B ubuntu mirror after refresh = %v, want serverB prefix", got)
	}
}
