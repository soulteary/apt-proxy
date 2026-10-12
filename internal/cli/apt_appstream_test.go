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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestProductionAppStreamFilesRewriteAndCache(t *testing.T) {
	const root = "dists/stable/main/dep11/"
	for _, distribution := range []struct {
		name string
		mode int
	}{{"ubuntu", distro.TypeUbuntu}, {"ubuntu-ports", distro.TypeUbuntuPorts}, {"debian", distro.TypeDebian}} {
		for _, yamlRules := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/yaml=%v", distribution.name, yamlRules), func(t *testing.T) {
				prefix := "/" + distribution.name + "/"
				payload := []byte{0x00, 0x1f, 0x8b, 0xff, 0x78, 0x7a, 0x42}
				var hits atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					if r.URL.Path != prefix+root+"Components-amd64.yml.xz" && r.URL.Path != prefix+root+"CID-Index-amd64.json.gz" && r.URL.Path != prefix+root+"icons-64x64@2.tar.gz" && r.URL.Path != prefix+root+"Components-arm64.yml.gz" {
						t.Errorf("unexpected rewritten path: %s", r.URL.Path)
					}
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					if r.URL.Path == prefix+root+"Components-arm64.yml.gz" {
						w.Header().Set("Cache-Control", "no-store")
					}
					_, _ = w.Write(payload)
				}))
				defer upstream.Close()
				cfg := &config.Config{CacheDir: t.TempDir(), Mode: distribution.mode, Listen: "127.0.0.1:0"}
				switch distribution.mode {
				case distro.TypeUbuntu:
					cfg.Mirrors.Ubuntu = upstream.URL + prefix
				case distro.TypeUbuntuPorts:
					cfg.Mirrors.UbuntuPorts = upstream.URL + prefix
				case distro.TypeDebian:
					cfg.Mirrors.Debian = upstream.URL + prefix
				}
				if yamlRules {
					path, err := filepath.Abs("../../config/distributions.yaml")
					if err != nil {
						t.Fatal(err)
					}
					cfg.DistributionsConfigPath = path
				}
				srv, err := NewServer(cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = srv.shutdown() })
				get := func(path string, status int, cacheState string) {
					t.Helper()
					resp, err := srv.app.Test(httptest.NewRequest(http.MethodGet, prefix+path, nil))
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if resp.StatusCode != status {
						t.Fatalf("%s: status=%d body=%q", path, resp.StatusCode, body)
					}
					if status == http.StatusOK && !bytes.Equal(body, payload) {
						t.Fatalf("metadata body bytes changed: %s", path)
					}
					if cacheState != "" && (resp.Header.Get("X-Cache") != cacheState || resp.Header.Get("Cache-Control") != "max-age=3600") {
						t.Fatalf("%s: X-Cache=%s Cache-Control=%s", path, resp.Header.Get("X-Cache"), resp.Header.Get("Cache-Control"))
					}
				}
				for i, filename := range []string{"Components-amd64.yml.xz", "CID-Index-amd64.json.gz", "icons-64x64@2.tar.gz"} {
					before := hits.Load()
					get(root+filename, http.StatusOK, "MISS")
					deadline := time.Now().Add(2 * time.Second)
					for srv.cache.Stats().ItemCount < i+1 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					get(root+filename, http.StatusOK, "HIT")
					if hits.Load() != before+1 {
						t.Fatalf("warm metadata contacted upstream: %s", filename)
					}
				}
				before := hits.Load()
				get(root+"Components-amd64.yml.xz.exe", http.StatusNotFound, "")
				get("Components-amd64.yml.xz", http.StatusNotFound, "")
				if hits.Load() != before {
					t.Fatal("unsupported metadata reached upstream")
				}
				get(root+"Components-arm64.yml.gz", http.StatusOK, "")
				get(root+"Components-arm64.yml.gz", http.StatusOK, "")
				if hits.Load() != before+2 || srv.cache.Stats().ItemCount != 3 {
					t.Fatal("upstream no-store metadata was reused or cached")
				}
			})
		}
	}
}
