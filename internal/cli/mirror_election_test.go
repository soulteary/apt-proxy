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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// requestRecorder starts an upstream that answers with name and the requested
// path, and returns the requests it received as "METHOD path".
func requestRecorder(t *testing.T, name string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, name+" "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), requests...)
	}
}

// With mirror election off, the daemon fetches a distribution without a pinned
// mirror from its official address, at startup and after a refresh. It probes
// neither that address nor the listed mirror: the upstreams see the package
// requests and nothing else.
func TestMirrorElectionOffUsesOfficialAddressWithoutProbing(t *testing.T) {
	official, officialRequests := requestRecorder(t, "official")
	listed, listedRequests := requestRecorder(t, "listed")

	path := filepath.Join(t.TempDir(), "distributions.yaml")
	writeAt(t, path, fmt.Sprintf(`
distributions:
  - id: debian
    name: Debian
    type: 3
    url_pattern: "/debian(-security)?/(.+)$"
    benchmark_url: "dists/stable/Release"
    official_url: "%s/debian/"
    cache_rules:
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "%s/debian/"
`, official.URL, listed.URL))

	srv, err := NewServer(&config.Config{
		CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeDebian,
		DistributionsConfigPath: path,
		Mirrors:                 config.MirrorConfig{DisableElection: true},
		Security:                config.SecurityConfig{EnableAPIAuth: true, APIKey: "refresh-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	if srv.state.MirrorElection() {
		t.Fatal("the configuration did not turn mirror election off")
	}

	get := func(target string) {
		t.Helper()
		res, err := srv.testRequest(httptest.NewRequest(http.MethodGet, target, nil))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || string(body) != "official "+target {
			t.Fatalf("GET %s: status=%d body=%q err=%v, want it served from the official address", target, res.StatusCode, body, err)
		}
	}
	get("/debian/pool/main/a/apt/apt_3.0_amd64.deb")

	refresh := httptest.NewRequest(http.MethodPost, "/api/mirrors/refresh", nil)
	refresh.Header.Set("X-API-Key", "refresh-key")
	res, err := srv.testRequest(refresh)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("refresh: status=%d", res.StatusCode)
	}
	get("/debian/pool/main/b/bash/bash_5.2_amd64.deb")
	// The security archive is derived from the official address.
	get("/debian-security/pool/updates/main/o/openssl/openssl_3.5_amd64.deb")

	want := []string{
		"GET /debian/pool/main/a/apt/apt_3.0_amd64.deb",
		"GET /debian/pool/main/b/bash/bash_5.2_amd64.deb",
		"GET /debian-security/pool/updates/main/o/openssl/openssl_3.5_amd64.deb",
	}
	if got := officialRequests(); !reflect.DeepEqual(got, want) {
		t.Errorf("requests to the official address = %q, want only the package requests %q", got, want)
	}
	if got := listedRequests(); len(got) != 0 {
		t.Errorf("the listed mirror was contacted with election off: %q", got)
	}
}
