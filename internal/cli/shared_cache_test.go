//go:build unix

package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
)

func TestSharedRepositoryInstancesCoalesceVerifiedContent(t *testing.T) {
	body := []byte("verified repository index")
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	var calls atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") != "" {
			t.Error("local request ID reached shared origin")
		}
		calls.Add(1)
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(body)
	})
	origin := httptest.NewServer(serve)
	peer := httptest.NewServer(serve)
	defer peer.Close()
	defer origin.Close()
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	disk, locks := t.TempDir(), t.TempDir()
	var servers []*Server
	for i := 0; i < 2; i++ {
		base := origin.URL
		if i == 1 {
			base = peer.URL
		}
		srv, err := NewServer(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu, Mirrors: config.MirrorConfig{Ubuntu: base + "/ubuntu/"}, SharedCache: &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "ubuntu", Bases: []string{origin.URL + "/ubuntu/", peer.URL + "/ubuntu/"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, srv)
		t.Cleanup(func() { _ = srv.shutdown() })
	}
	path := "/ubuntu/dists/test/main/binary-arm64/by-hash/SHA256/" + digest
	responses := make(chan error, 2)
	get := func(srv *Server) {
		req := httptest.NewRequest("GET", path, nil)
		res, err := srv.app.Test(req)
		if err == nil {
			data, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if readErr != nil {
				err = readErr
			} else if res.StatusCode != 200 || string(data) != string(body) {
				err = io.ErrUnexpectedEOF
			}
		}
		responses <- err
	}
	go get(servers[0])
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not fetch")
	}
	go get(servers[1])
	unblock.Do(func() { close(release) })
	for range 2 {
		if err := <-responses; err != nil {
			t.Fatal(err)
		}
	}
	for _, srv := range servers {
		if err := srv.cacheProxy.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("shared fill contacted origin %d times", calls.Load())
	}
	if err := servers[0].cache.Purge(); err == nil {
		t.Fatal("unsafe online purge accepted")
	}
}

func TestSharedRepositoryRejectsUnknownTenantHeaders(t *testing.T) {
	body := []byte("same public SHA256 index body")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.Header.Get("X-Tenant-ID")
		if tenant == "" {
			tenant = "public"
		}
		w.Header().Set("X-Tenant-Secret", tenant)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}))
	defer origin.Close()
	disk, locks := t.TempDir(), t.TempDir()
	newServer := func() *Server {
		srv, err := NewServer(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu, Mirrors: config.MirrorConfig{Ubuntu: origin.URL + "/ubuntu/"}, SharedCache: &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "ubuntu", Bases: []string{origin.URL + "/ubuntu/"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.shutdown() })
		return srv
	}
	one, two := newServer(), newServer()
	get := func(srv *Server, tenant string) (string, string) {
		r := httptest.NewRequest("GET", "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/"+digest, nil)
		if tenant != "" {
			r.Header.Set("X-Tenant-ID", tenant)
		}
		res, err := srv.app.Test(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		if tenant != "" && res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("private response was cacheable")
		}
		return res.Header.Get("X-Tenant-Secret"), res.Header.Get("X-Cache")
	}
	if tenant, _ := get(one, "alice"); tenant != "alice" {
		t.Fatalf("first request %q", tenant)
	}
	if err := one.cacheProxy.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tenant, cache := get(two, ""); tenant != "public" {
		t.Fatalf("anonymous received tenant header %q (cache=%s)", tenant, cache)
	}
}

func TestSharedRepositoryCrossInstance304Withdrawal(t *testing.T) {
	body := []byte("same immutable public SHA256 index body")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	var withdraw atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if withdraw.Load() {
			w.Header().Set("Cache-Control", "no-store")
			if r.Header.Get("If-None-Match") != "" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		} else {
			w.Header().Set("Cache-Control", "public,max-age=3600")
		}
		_, _ = w.Write(body)
	}))
	defer origin.Close()
	disk, locks := t.TempDir(), t.TempDir()
	newServer := func() *Server {
		srv, err := NewServer(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu, Mirrors: config.MirrorConfig{Ubuntu: origin.URL + "/ubuntu/"}, SharedCache: &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "ubuntu", Bases: []string{origin.URL + "/ubuntu/"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.shutdown() })
		return srv
	}
	one, two := newServer(), newServer()
	get := func(srv *Server, refresh bool) (string, string) {
		req := httptest.NewRequest("GET", "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/"+digest, nil)
		if refresh {
			req.Header.Set("Cache-Control", "min-fresh=7200")
		}
		res, err := srv.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		return res.Header.Get("Cache-Control"), res.Header.Get("X-Cache")
	}
	get(one, false)
	if err := one.cacheProxy.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	withdraw.Store(true)
	if cc, _ := get(one, true); cc != "no-store" {
		t.Fatalf("withdrawal not reached: %q", cc)
	}
	if cc, cache := get(two, false); cc != "no-store" || cache == "HIT" {
		t.Fatalf("second instance ignored 304 permission withdrawal: cc=%q cache=%s", cc, cache)
	}
}
