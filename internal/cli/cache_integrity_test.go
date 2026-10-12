package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestProductionChecksumRejectsUnknownMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	_, err := NewServer(&config.Config{CacheDir: dir, Cache: config.CacheConfig{Integrity: "sha257"}})
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("unknown integrity policy accepted: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid policy initialized storage before being rejected")
	}
}

func checksumServer(t *testing.T, dir, mirror, mode string) *Server {
	t.Helper()
	s, err := NewServer(&config.Config{CacheDir: dir, Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: mirror + "/ubuntu/"}, Cache: config.CacheConfig{Integrity: mode}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.shutdown() })
	return s
}
func checksumRequest(t *testing.T, s *Server, want, cacheLabel string) {
	t.Helper()
	res, err := s.app.Test(httptest.NewRequest("GET", "/ubuntu/pool/integrity.deb", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	httpcache.Writes.Wait()
	if err != nil || res.StatusCode != 200 || string(body) != want || res.Header.Get("X-Cache") != cacheLabel {
		t.Fatalf("response=%d label=%q body=%q err=%v", res.StatusCode, res.Header.Get("X-Cache"), body, err)
	}
}
func TestProductionChecksumRefetchesSameLengthTampering(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "restart"}[restart], func(t *testing.T) {
			var calls atomic.Int32
			const original, updated = "package-original", "package-updated!"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := original
				if calls.Add(1) > 1 {
					payload = updated
				}
				w.Header().Set("Content-Length", "16")
				w.Header().Set("Cache-Control", "max-age=3600")
				_, _ = io.WriteString(w, payload)
			}))
			defer upstream.Close()
			dir := t.TempDir()
			s := checksumServer(t, dir, upstream.URL, "")
			checksumRequest(t, s, original, "MISS")
			paths, err := filepath.Glob(filepath.Join(dir, "body", "v1", "*"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("body entries=%v %v", paths, err)
			}
			if err := os.WriteFile(paths[0], bytes.Repeat([]byte("x"), len(original)), 0600); err != nil {
				t.Fatal(err)
			}
			if restart {
				if err := s.shutdown(); err != nil {
					t.Fatal(err)
				}
				s = checksumServer(t, dir, upstream.URL, "")
			}
			checksumRequest(t, s, updated, "MISS")
			checksumRequest(t, s, updated, "HIT")
			if calls.Load() != 2 {
				t.Fatalf("checksum refetch origin count=%d", calls.Load())
			}
		})
	}
}
func TestProductionChecksumLegacyMigrationAndExplicitOptOut(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", "16")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = io.WriteString(w, "package-original")
	}))
	defer upstream.Close()
	dir := t.TempDir()
	legacy := checksumServer(t, dir, upstream.URL, "none")
	checksumRequest(t, legacy, "package-original", "MISS")
	checksumRequest(t, legacy, "package-original", "HIT")
	headers, err := filepath.Glob(filepath.Join(dir, "header", "v1", "*"))
	if err != nil || len(headers) != 1 {
		t.Fatal("missing legacy entry")
	}
	raw, err := os.ReadFile(headers[0])
	if err != nil || !bytes.HasPrefix(raw, []byte("HTTPCACHE/1 ")) {
		t.Fatalf("explicit opt-out not honored: %q %v", raw, err)
	}
	if err := legacy.shutdown(); err != nil {
		t.Fatal(err)
	}
	verified := checksumServer(t, dir, upstream.URL, "")
	checksumRequest(t, verified, "package-original", "MISS")
	checksumRequest(t, verified, "package-original", "HIT")
	if calls.Load() != 2 {
		t.Fatalf("unverified legacy entry reused after upgrade: upstream=%d", calls.Load())
	}
	raw, err = os.ReadFile(headers[0])
	if err != nil || !bytes.HasPrefix(raw, []byte("HTTPCACHE/2 ")) {
		t.Fatalf("private digest missing after migration: %q %v", raw, err)
	}
}
