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
	"testing"

	httpcache "github.com/soulteary/httpcache-kit"

	"github.com/soulteary/apt-proxy/internal/config"
)

// TestCacheLabelFromHeader exercises the small helper that normalises
// the X-Cache header for log fields. It used to be 37.5% covered;
// missing branches were "MISS" and arbitrary values.
func TestCacheLabelFromHeader(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "SKIP"},
		{"  ", "SKIP"},
		{"HIT", "HIT"},
		{"HIT from disk", "HIT"},
		{"MISS", "MISS"},
		{"MISS from upstream", "MISS"},
		{"BYPASS", "BYPASS"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := cacheLabelFromHeader(c.in); got != c.want {
				t.Errorf("cacheLabelFromHeader(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSetBuildInfo verifies the package-level build metadata is
// updated only when non-empty values are supplied; empty arguments
// must preserve any prior value (so a partially-populated -ldflags
// invocation doesn't clobber other fields).
func TestSetBuildInfo(t *testing.T) {
	// Snapshot existing values to restore at the end of the test.
	origV, origC, origD := BuildInfo()
	t.Cleanup(func() {
		buildVersion = origV
		buildCommit = origC
		buildDate = origD
	})

	SetBuildInfo("v1.2.3", "abcdef0", "2026-06-06")
	v, c, d := BuildInfo()
	if v != "v1.2.3" || c != "abcdef0" || d != "2026-06-06" {
		t.Fatalf("after full SetBuildInfo: %q %q %q", v, c, d)
	}

	// Empty arguments must NOT overwrite the prior value.
	SetBuildInfo("", "newcommit", "")
	v, c, d = BuildInfo()
	if v != "v1.2.3" || c != "newcommit" || d != "2026-06-06" {
		t.Fatalf("after partial SetBuildInfo: %q %q %q", v, c, d)
	}
}

// TestInitCacheUnknownBackend covers the error path for a bogus
// storage.backend value. Previously initCache had only the disk path
// covered (15.4%); this exercises the default-case fallthrough.
func TestInitCacheUnknownBackend(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		CacheDir: tmp,
		Storage: config.StorageConfig{
			Backend: "magneto-tape",
		},
	}
	srv := &Server{config: cfg}
	srv.initLogger()

	if _, err := srv.initCache(httpcache.DefaultCacheConfig()); err == nil {
		t.Fatal("initCache with unknown backend should return error, got nil")
	}
}

// TestInitCacheDiskBackend exercises both the empty-string default
// and the explicit "disk" value to confirm they both succeed and
// don't accidentally take the s3 branch.
func TestInitCacheDiskBackend(t *testing.T) {
	for _, backend := range []string{"", config.StorageBackendDisk} {
		t.Run("backend="+backend, func(t *testing.T) {
			tmp := t.TempDir()
			cfg := &config.Config{
				CacheDir: tmp,
				Storage:  config.StorageConfig{Backend: backend},
			}
			srv := &Server{config: cfg}
			srv.initLogger()
			cache, err := srv.initCache(httpcache.DefaultCacheConfig())
			if err != nil {
				t.Fatalf("initCache(%q): %v", backend, err)
			}
			if cache == nil {
				t.Fatalf("initCache(%q) returned nil cache", backend)
			}
			_ = cache.Close()
		})
	}
}

// TestShutdownIsSafeAfterPartialInit exercises the defensive paths in
// shutdown(): even when called on a Server that has not had cache
// initialised, it must not panic and must still attempt tracing
// shutdown. This pins the "all cleanup steps run unconditionally"
// contract documented in shutdown().
func TestShutdownIsSafeAfterPartialInit(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		CacheDir: tmp,
		Listen:   "127.0.0.1:0",
	}
	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if err := srv.shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
