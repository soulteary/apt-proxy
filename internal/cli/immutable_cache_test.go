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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// The daemon wiring adds the staging budget, the retrying transport and
// download admission around the cases covered in internal/proxy. Requests go
// to the repository handler directly, so the test does not depend on the HTTP
// server in front of it.
func TestProductionImmutableArtifactCaching(t *testing.T) {
	var failing atomic.Bool
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case failing.Load():
			http.Error(w, "mirror unavailable", http.StatusServiceUnavailable)
		case strings.Contains(r.URL.Path, "/by-hash/"):
			if r.Header.Get("Cache-Control") != "" {
				t.Errorf("revalidation directive reached the origin: %v", r.Header)
			}
			_, _ = io.WriteString(w, "index")
		default:
			// Age beyond max-age: the entry is expired as soon as it is stored.
			w.Header().Set("Cache-Control", "max-age=60")
			w.Header().Set("Age", "3600")
			w.Header().Set("Last-Modified", "Wed, 01 Jan 2020 00:00:00 GMT")
			_, _ = io.WriteString(w, "package")
		}
	}))
	defer upstream.Close()
	cfg := &config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0"}
	cfg.Mirrors.Ubuntu = upstream.URL + "/ubuntu/"
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	stored := 0
	request := func(path, directive, body, cacheState string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if directive != "" {
			req.Header.Set("Cache-Control", directive)
		}
		// The retrying transport spends its backoff on a failing origin.
		rec := httptest.NewRecorder()
		srv.proxy.ServeHTTP(rec, req)
		resp := rec.Result()
		got, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || string(got) != body || resp.Header.Get("X-Cache") != cacheState {
			t.Fatalf("%s: status=%d body=%q cache=%s err=%v", path, resp.StatusCode, got, resp.Header.Get("X-Cache"), err)
		}
		if cacheState != "MISS" {
			return
		}
		stored++
		deadline := time.Now().Add(2 * time.Second)
		for srv.cache.Stats().ItemCount < stored && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if srv.cache.Stats().ItemCount < stored {
			t.Fatalf("%s: response not stored", path)
		}
	}
	// APT marks every index request, by-hash ones included, with max-age=0.
	index := "/ubuntu/dists/jammy/main/binary-amd64/by-hash/SHA256/" + strings.Repeat("a", 64)
	request(index, "max-age=0", "index", "MISS")
	request(index, "max-age=0", "index", "HIT")
	if hits.Load() != 1 {
		t.Fatalf("origin requests for the index = %d, want 1", hits.Load())
	}
	const artifact = "/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb"
	request(artifact, "", "package", "MISS")
	failing.Store(true)
	before := hits.Load()
	request(artifact, "", "package", "HIT")
	if hits.Load() == before {
		t.Fatal("expired package served without revalidation")
	}
	// The replaced upstream response holds a download slot until it is closed.
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 0 {
		t.Fatalf("failed revalidation retained resources: %+v", stats)
	}
}
