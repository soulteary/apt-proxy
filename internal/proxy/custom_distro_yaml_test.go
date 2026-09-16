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

package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// The sibling custom_distro_test.go drives the registry in memory. These
// tests take the route the README documents instead: a distributions.yaml on
// disk, loaded through Registry.Reload, declaring a distribution apt-proxy
// does not ship. That is the answer to "how do I cache <distro>?" for Deepin,
// Armbian and anything else outside the built-in five, so it is worth holding
// the whole file-to-upstream chain in place.

// writeDistributionsYAML drops a distributions.yaml in a temp dir and returns
// its path.
func writeDistributionsYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "distributions.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// echoUpstream answers every request with 200 and records the last path seen.
func echoUpstream(t *testing.T) (url string, lastPath *string) {
	t.Helper()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &seen
}

func serveThrough(t *testing.T, reg *distro.Registry, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	st := newTestState()
	st.SetProxyMode(distro.TypeAllDistros)

	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: reg,
		CacheDir: t.TempDir(),
		Logger:   logger.Default(),
		Mode:     distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)
	return rec
}

// A distribution with a type outside the built-in 1..5 range is registered
// from YAML and proxied like any other.
func TestDistributionsYAMLRegistersNewDistribution(t *testing.T) {
	upstream, gotPath := echoUpstream(t)

	path := writeDistributionsYAML(t, fmt.Sprintf(`
distributions:
  - id: deepin
    name: Deepin
    type: 6
    url_pattern: "/deepin/(.+)$"
    benchmark_url: "dists/apricot/main/binary-amd64/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "InRelease$"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "%s/deepin/"
`, upstream))

	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(path); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	d, ok := reg.GetByType(6)
	if !ok {
		t.Fatal("type 6 was not registered from distributions.yaml")
	}
	if d.ID != "deepin" {
		t.Errorf("type 6 registered as %q, want %q", d.ID, "deepin")
	}

	rec := serveThrough(t, reg, httptest.NewRequest(http.MethodGet,
		"http://apt-proxy.example/deepin/dists/apricot/InRelease", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the request never reached the configured mirror)", rec.Code)
	}
	if want := "/deepin/dists/apricot/InRelease"; *gotPath != want {
		t.Errorf("upstream saw %q, want %q", *gotPath, want)
	}
}

// An archive that lives at a domain root has no path prefix for url_pattern
// to match, so the entry carries host_pattern instead. apt writes
//
//	deb http://apt.armbian.com <suite> main
//
// which arrives as /dists/<suite>/... with the archive named only by Host.
func TestDistributionsYAMLHostRootArchive(t *testing.T) {
	upstream, gotPath := echoUpstream(t)

	path := writeDistributionsYAML(t, fmt.Sprintf(`
distributions:
  - id: armbian
    name: Armbian
    type: 7
    url_pattern: "/armbian/(.+)$"
    host_pattern: "^apt\\.armbian\\.com(:\\d+)?$"
    benchmark_url: "dists/bookworm/main/binary-arm64/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "InRelease$"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "%s/armbian/"
`, upstream))

	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(path); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dists/bookworm/InRelease", nil)
	req.Host = "apt.armbian.com"

	rec := serveThrough(t, reg, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (host-root archive did not route)", rec.Code)
	}
	if want := "/armbian/dists/bookworm/InRelease"; *gotPath != want {
		t.Errorf("upstream saw %q, want %q", *gotPath, want)
	}
}

// A type already taken by a built-in must be rejected rather than silently
// shadowing it, so operators pick a free number.
func TestDistributionsYAMLRejectsTypeCollision(t *testing.T) {
	path := writeDistributionsYAML(t, `
distributions:
  - id: not-debian
    name: Not Debian
    type: 3
    url_pattern: "/not-debian/(.+)$"
    benchmark_url: "dists/bookworm/Release"
    cache_rules:
      - pattern: "InRelease$"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "mirrors.example.com/not-debian/"
`)

	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(path); err == nil {
		t.Error("reusing built-in type 3 must be an error, got nil")
	}
}
