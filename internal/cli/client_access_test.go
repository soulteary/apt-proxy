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

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestProductionDownloadClientAccess(t *testing.T) {
	for _, tc := range []struct {
		name     string
		security config.SecurityConfig
		status   int
	}{
		{"default", config.SecurityConfig{}, 200},
		{"allowed", config.SecurityConfig{DownloadAllowCIDRs: []string{"127.0.0.0/8"}}, 200},
		{"not allowed", config.SecurityConfig{DownloadAllowCIDRs: []string{"192.0.2.0/24"}, TrustedProxies: []string{"127.0.0.0/8"}}, 403},
		{"deny precedence", config.SecurityConfig{DownloadAllowCIDRs: []string{"127.0.0.0/8"}, DownloadDenyCIDRs: []string{"127.0.0.1/32"}}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _, _ = io.WriteString(w, "repository-data") }))
			defer upstream.Close()
			srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}, Security: tc.security})
			if err != nil {
				t.Fatal(err)
			}
			// Warm the real cache directly; the listener must still deny warm hits.
			warm := httptest.NewRecorder()
			srv.proxy.ServeHTTP(warm, httptest.NewRequest("GET", "http://archive.ubuntu.com/ubuntu/dists/jammy/InRelease", nil))
			if warm.Code != 200 {
				t.Fatalf("warm status=%d", warm.Code)
			}
			waitForCacheStore(t, srv)
			baseline := hits.Load()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- srv.app.Listener(listener) }()
			t.Cleanup(func() {
				_ = srv.shutdown()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("listener did not stop")
				}
			})
			client := &http.Client{Timeout: 5 * time.Second}
			for _, path := range []string{"/ubuntu/dists/jammy/InRelease", "/ubuntu/dists/noble/InRelease"} {
				req, err := http.NewRequest("GET", "http://"+listener.Addr().String()+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("X-Forwarded-For", "192.0.2.1")
				req.Header.Set("X-Real-IP", "192.0.2.1")
				req.Header.Set("Forwarded", "for=192.0.2.1")
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != tc.status {
					t.Fatalf("%s: status=%d body=%q", path, resp.StatusCode, body)
				}
				if tc.status == 403 && resp.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("denial is cacheable")
				}
			}
			if tc.status == 403 && hits.Load() != baseline {
				t.Fatalf("denied client reached upstream: %d -> %d", baseline, hits.Load())
			}
			if tc.status == 200 && hits.Load() != baseline+1 {
				t.Fatalf("allowed warm/cold behavior: hits=%d baseline=%d", hits.Load(), baseline)
			}
			for _, path := range []string{"/_/ping", "/version", "/api/cache/stats"} {
				resp, err := client.Get("http://" + listener.Addr().String() + path)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatalf("operational route %s: %d", path, resp.StatusCode)
				}
			}
		})
	}
}
func TestNewServerRejectsInvalidDownloadAccess(t *testing.T) {
	for _, security := range []config.SecurityConfig{{DownloadAllowCIDRs: []string{"invalid"}}, {DownloadDenyCIDRs: []string{""}}} {
		if _, err := NewServer(&config.Config{Security: security}); err == nil {
			t.Fatal("invalid ACL accepted")
		}
	}
}
