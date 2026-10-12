//go:build unix

package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestSharedByHashMissingMirrorDoesNotPoisonHealthyMirror(t *testing.T) {
	body := []byte("verified index available on healthy mirror")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	var missingCalls, healthyCalls atomic.Int32
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		missingCalls.Add(1)
		// An explicit upstream freshness policy must not publish an unverified
		// representation under a content identity shared with healthy mirrors.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "unavailable on this mirror")
	}))
	defer missing.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyCalls.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}))
	defer healthy.Close()
	disk, locks := t.TempDir(), t.TempDir()
	bases := []string{missing.URL + "/ubuntu/", healthy.URL + "/ubuntu/"}
	makeServer := func(base string) *Server {
		srv, err := NewServer(withTestMirrors(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
			Mirrors:     config.MirrorConfig{Ubuntu: base},
			SharedCache: &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "public", Bases: bases}}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.shutdown() })
		return srv
	}
	one, two := makeServer(bases[0]), makeServer(bases[1])
	path := "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/" + digest
	res, err := one.app.Test(httptest.NewRequest("GET", path, nil))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	first := sharedResult{status: res.StatusCode, body: string(data), label: res.Header.Get("X-Cache"), err: err}
	if first.err != nil || first.status != 404 {
		t.Fatalf("missing response changed: %+v", first)
	}
	httpcache.Writes.Wait()
	second := getShared(two, path)
	if second.err != nil || second.status != 200 || second.body != string(body) || healthyCalls.Load() != 1 || second.label == "HIT" {
		t.Fatalf("missing mirror poisoned healthy shared identity: first=%+v second=%+v missingCalls=%d healthyCalls=%d", first, second, missingCalls.Load(), healthyCalls.Load())
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("outer repository policy replaced forced no-store: %q", cc)
	}
	httpcache.Writes.Wait()
	third := getShared(two, path)
	if third.err != nil || third.status != 200 || third.body != string(body) || third.label != "HIT" || healthyCalls.Load() != 1 {
		t.Fatalf("verified healthy body was not reusable: %+v healthyCalls=%d", third, healthyCalls.Load())
	}
}

func TestSharedByHash304FreshensExistingVerifiedBody(t *testing.T) {
	body := []byte("verified body retained across conditional refresh")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	var conditional atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"index-v1"`)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if r.Header.Get("If-None-Match") == `"index-v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	}))
	defer origin.Close()
	srv, err := NewServer(withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
		Mirrors:     config.MirrorConfig{Ubuntu: origin.URL + "/ubuntu/"},
		SharedCache: &sharedcache.Config{CoordinationDir: t.TempDir(), Repositories: []sharedcache.Repository{{ID: "public", Bases: []string{origin.URL + "/ubuntu/"}}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.shutdown() }()
	path := "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/" + digest
	if first := getShared(srv, path); first.err != nil || first.status != 200 || first.body != string(body) {
		t.Fatalf("initial verified fill: %+v", first)
	}
	httpcache.Writes.Wait()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Cache-Control", "min-fresh=7200")
	res, err := srv.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	httpcache.Writes.Wait()
	if err != nil || res.StatusCode != 200 || string(data) != string(body) || conditional.Load() != 1 || res.Header.Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("valid304 did not retain verified body/freshness: status=%d body=%q cc=%q err=%v conditional=%d", res.StatusCode, data, res.Header.Get("Cache-Control"), err, conditional.Load())
	}
	if final := getShared(srv, path); final.err != nil || final.status != 200 || final.body != string(body) || final.label != "HIT" {
		t.Fatalf("freshened verified hit failed: %+v", final)
	}
}

func TestSharedByHashCold304CannotPoisonHealthyMirror(t *testing.T) {
	body := []byte("verified body after a cold unsolicited304")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	var healthyCalls atomic.Int32
	cold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("ETag", `"cold-v1"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer cold.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyCalls.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}))
	defer healthy.Close()
	disk, locks := t.TempDir(), t.TempDir()
	bases := []string{cold.URL + "/ubuntu/", healthy.URL + "/ubuntu/"}
	makeServer := func(base string) *Server {
		srv, err := NewServer(withTestMirrors(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
			Mirrors:     config.MirrorConfig{Ubuntu: base},
			SharedCache: &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "public", Bases: bases}}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.shutdown() })
		return srv
	}
	one, two := makeServer(bases[0]), makeServer(bases[1])
	path := "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/" + digest
	if first := getShared(one, path); first.err != nil || first.status != 304 || first.body != "" {
		t.Fatalf("cold304 response changed: %+v", first)
	}
	httpcache.Writes.Wait()
	if second := getShared(two, path); second.err != nil || second.status != 200 || second.body != string(body) || second.label == "HIT" || healthyCalls.Load() != 1 {
		t.Fatalf("cold304 became an unverified shared representation: %+v healthyCalls=%d", second, healthyCalls.Load())
	}
	httpcache.Writes.Wait()
	if third := getShared(two, path); third.err != nil || third.status != 200 || third.body != string(body) || third.label != "HIT" || healthyCalls.Load() != 1 {
		t.Fatalf("verified healthy body was not reusable after cold304: %+v healthyCalls=%d", third, healthyCalls.Load())
	}
}

func TestSharedHeadFallbackThroughMirror304PreservesAndWithdrawsGet(t *testing.T) {
	const manifest = "same snapshot for both public mirrors"
	body := []byte("verified shared GET body retained through mirror HEAD304")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	var failed, withdraw atomic.Bool
	var candidateConditionals atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if r.URL.Path == "/ubuntu/dists/test/InRelease" {
			_, _ = io.WriteString(w, manifest)
			return
		}
		if failed.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("ETag", `"shared-v1"`)
		_, _ = w.Write(body)
	}))
	defer primary.Close()
	candidate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if r.URL.Path == "/archive/dists/test/InRelease" {
			_, _ = io.WriteString(w, manifest)
			return
		}
		if withdraw.Load() {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("ETag", `"shared-v1"`)
		if r.Header.Get("If-None-Match") == `"shared-v1"` {
			candidateConditionals.Add(1)
			// A 304 Content-Length can describe the cached GET representation.
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.WriteHeader(304)
			return
		}
		_, _ = w.Write(body)
	}))
	defer candidate.Close()
	disk, locks := t.TempDir(), t.TempDir()
	bases := []string{primary.URL + "/ubuntu/", candidate.URL + "/archive/"}
	makeServer := func(base string) *Server {
		cfg := withTestMirrors(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
			SharedCache:  &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "public", Bases: bases}}},
			MirrorHealth: &mirrorhealth.Config{Enabled: true, FailureThreshold: 1, Repositories: []mirrorhealth.Repository{{ID: "ubuntu", Mirrors: bases}}},
		})
		cfg.Mirrors.Ubuntu = base
		srv, err := NewServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.shutdown() })
		return srv
	}
	one, two := makeServer(bases[0]), makeServer(bases[1])
	if res := getShared(one, "/ubuntu/dists/test/InRelease"); res.err != nil || res.status != 200 || res.body != manifest {
		t.Fatalf("snapshot observation failed: %+v", res)
	}
	path := "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/" + digest
	if res := getShared(one, path); res.err != nil || res.status != 200 || res.body != string(body) {
		t.Fatalf("verified initialGET: %+v", res)
	}
	httpcache.Writes.Wait()
	failed.Store(true)
	refreshHead := func() string {
		t.Helper()
		req := httptest.NewRequest("HEAD", path, nil)
		req.Header.Set("Cache-Control", "min-fresh=7200")
		res, err := one.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		httpcache.Writes.Wait()
		if err != nil || res.StatusCode != 200 || len(data) != 0 {
			t.Fatalf("HEAD fallback returned invalid representation: status=%d body=%q err=%v", res.StatusCode, data, err)
		}
		return res.Header.Get("Cache-Control")
	}
	if cc := refreshHead(); cc != "public, max-age=3600" {
		t.Fatalf("mirror304 wrappers changed freshening policy: %q", cc)
	}
	if candidateConditionals.Load() != 1 {
		t.Fatalf("HEAD did not switch through candidate304: conditionals=%d", candidateConditionals.Load())
	}
	if res := getShared(two, path); res.err != nil || res.status != 200 || res.body != string(body) || res.label != "HIT" {
		t.Fatalf("HEAD freshening lost sharedGET: %+v", res)
	}
	withdraw.Store(true)
	if cc := refreshHead(); cc != "no-store" {
		t.Fatalf("HEAD304 withdrawal changed: %q", cc)
	}
	if res := getShared(two, path); res.err != nil || res.status != 200 || res.body != string(body) || res.label == "HIT" {
		t.Fatalf("HEAD304 withdrawal failed to fence sharedGET in peer: %+v", res)
	}
	httpcache.Writes.Wait()
	for _, srv := range []*Server{one, two} {
		if stats := srv.resources.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
			t.Fatalf("wrapped304 retained resources: %+v", stats)
		}
	}
}
