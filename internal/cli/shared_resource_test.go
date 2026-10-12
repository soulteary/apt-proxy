//go:build unix

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
	"github.com/soulteary/apt-proxy/internal/storage"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// Embedding VerifiedCache preserves its ContextStore and stale-marker methods.
// The signal proves the follower saw a cold miss before the leader can commit.
type observedSharedMiss struct {
	*storage.VerifiedCache
	missed chan struct{}
	once   sync.Once
}

func (c *observedSharedMiss) Retrieve(key string) (*httpcache.Resource, error) {
	res, err := c.VerifiedCache.Retrieve(key)
	if errors.Is(err, httpcache.ErrNotFoundInCache) {
		c.once.Do(func() { close(c.missed) })
	}
	return res, err
}

type sharedResult struct {
	status      int
	body, label string
	err         error
}

func getShared(s *Server, path string) sharedResult {
	res, err := s.app.Test(httptest.NewRequest("GET", path, nil), fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		return sharedResult{err: err}
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return sharedResult{status: res.StatusCode, body: string(body), label: res.Header.Get("X-Cache"), err: err}
}
func awaitSharedSignal(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(what)
	}
}

func TestSharedColdIdentityCoalescesThroughFailoverAndResourceBudget(t *testing.T) {
	const manifest = "same public repository manifest"
	body := []byte("verified shared index from healthy candidate")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	var primaryPackages, probes, candidatePackages atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ubuntu/dists/test/InRelease" {
			_, _ = io.WriteString(w, manifest)
			return
		}
		primaryPackages.Add(1)
		w.WriteHeader(503)
	}))
	defer primary.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	releaseBody := func() { unblock.Do(func() { close(release) }) }
	candidate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/dists/test/InRelease" {
			probes.Add(1)
			_, _ = io.WriteString(w, manifest)
			return
		}
		candidatePackages.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(body[:8])
		w.(http.Flusher).Flush()
		close(entered)
		select {
		case <-release:
			_, _ = w.Write(body[8:])
		case <-r.Context().Done():
		}
	}))
	defer candidate.Close()
	defer releaseBody()
	disk, locks := t.TempDir(), t.TempDir()
	bases := []string{primary.URL + "/ubuntu/", candidate.URL + "/archive/"}
	makeServer := func(mirror string) *Server {
		cfg := withTestMirrors(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
			Cache:        config.CacheConfig{MaxSize: 1, TTL: time.Nanosecond, CleanupInterval: time.Nanosecond, Integrity: "none"},
			Resources:    config.ResourceConfig{MaxObjectMB: 1, TotalTempMB: 1, MaxActiveDownloads: 1, MinFreeMB: 1},
			SharedCache:  &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "public", Bases: bases}}},
			MirrorHealth: &mirrorhealth.Config{Enabled: true, FailureThreshold: 1, Repositories: []mirrorhealth.Repository{{ID: "ubuntu", Mirrors: bases}}},
		})
		cfg.Mirrors.Ubuntu = mirror
		srv, err := NewServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.shutdown() })
		return srv
	}
	leader := makeServer(bases[0])
	// A second instance must not sweep another instance's active native stage.
	stage := filepath.Join(disk, "staging", "v1", "other-process.tmp")
	if err := os.MkdirAll(filepath.Dir(stage), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stage, []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	follower := makeServer(bases[1])
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("second instance swept a peer stage: %v", err)
	}
	if result := getShared(leader, "/ubuntu/dists/test/InRelease"); result.err != nil || result.status != 200 || result.body != manifest {
		t.Fatalf("manifest observation failed: %+v", result)
	}
	httpcache.Writes.Wait()
	locked := follower.cache.(*sharedcache.LockedCache)
	observer := &observedSharedMiss{VerifiedCache: locked.ExtendedCache.(*storage.VerifiedCache), missed: make(chan struct{})}
	locked.ExtendedCache = observer
	path := "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/" + digest
	results := make(chan sharedResult, 2)
	var requests sync.WaitGroup
	start := func(s *Server) { requests.Add(1); go func() { defer requests.Done(); results <- getShared(s, path) }() }
	defer func() { releaseBody(); requests.Wait() }()
	start(leader)
	awaitSharedSignal(t, entered, "leader did not reach candidate")
	if stats := leader.resources.Stats(); stats.ActiveDownloads != 1 {
		t.Fatalf("leader lost admission: %+v", stats)
	}
	start(follower)
	awaitSharedSignal(t, observer.missed, "follower did not observe cold shared identity")
	releaseBody()
	labels := map[string]int{}
	for range 2 {
		select {
		case res := <-results:
			if res.err != nil || res.status != 200 || res.body != string(body) {
				t.Fatalf("shared response failed: %+v", res)
			}
			labels[res.label]++
		case <-time.After(5 * time.Second):
			t.Fatal("shared requests did not finish")
		}
	}
	httpcache.Writes.Wait()
	if labels["MISS"] != 1 || labels["HIT"] != 1 || candidatePackages.Load() != 1 || probes.Load() != 1 || primaryPackages.Load() == 0 {
		t.Fatalf("cold misses did not coalesce through failover: labels=%v primary=%d probes=%d bodies=%d", labels, primaryPackages.Load(), probes.Load(), candidatePackages.Load())
	}
	for _, s := range []*Server{leader, follower} {
		stats := s.resources.Stats()
		if stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 || stats.PeakTemporaryBytes > 1<<20 {
			t.Fatalf("shared request retained/exceeded resources: %+v", stats)
		}
	}
	headers, err := filepath.Glob(filepath.Join(disk, "header", "v1", "*"))
	if err != nil || len(headers) == 0 {
		t.Fatalf("no shared cache records: %v %v", headers, err)
	}
	for _, name := range headers {
		data, err := os.ReadFile(name)
		if err != nil || !bytes.HasPrefix(data, []byte("HTTPCACHE/2 ")) {
			t.Fatalf("shared mode disabled mandatory integrity: %s %v", name, err)
		}
	}
}

func TestSharedVerificationCancellationReleasesAllResourcesAndMissLock(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 256<<10)
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	entered, originCanceled := make(chan struct{}), make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(body[:128<<10])
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(originCanceled)
	}))
	defer origin.Close()
	disk, locks := t.TempDir(), t.TempDir()
	cfg := withTestMirrors(&config.Config{CacheDir: disk, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
		Resources:   config.ResourceConfig{MaxObjectMB: 1, TotalTempMB: 1, MaxActiveDownloads: 1, MinFreeMB: 1},
		SharedCache: &sharedcache.Config{CoordinationDir: locks, Repositories: []sharedcache.Repository{{ID: "public", Bases: []string{origin.URL + "/ubuntu/"}}}},
	})
	cfg.Mirrors.Ubuntu = origin.URL + "/ubuntu/"
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.shutdown() }()
	finished := make(chan struct{})
	// Real net/http preserves cancellation; Fiber's Test adapter serializes requests.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); srv.proxy.ServeHTTP(w, r) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := "/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/" + digest
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		res, err := server.Client().Do(request)
		if res != nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
		result <- err
	}()
	awaitSharedSignal(t, entered, "verification did not reach origin")
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for srv.resources.Stats().ByKind[sharedcache.StageByHash] == 0 {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("verification never reserved shared bytes")
		}
	}
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 1 {
		t.Fatalf("verification lost active admission: %+v", stats)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request cancellation failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client did not cancel")
	}
	awaitSharedSignal(t, originCanceled, "origin not canceled")
	awaitSharedSignal(t, finished, "proxy verification did not finish")
	httpcache.Writes.Wait()
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
		t.Fatalf("canceled verifier retained resources: %+v", stats)
	}
	bodies, err := filepath.Glob(filepath.Join(disk, "body", "v1", "*"))
	if err != nil || len(bodies) != 0 {
		t.Fatalf("canceled verification published a body: %v %v", bodies, err)
	}
	policy, err := sharedcache.New(cfg.SharedCache)
	if err != nil {
		t.Fatal(err)
	}
	key, err := policy.RequestKey(httptest.NewRequest("GET", origin.URL+path, nil))
	if err != nil {
		t.Fatal(err)
	}
	lockCtx, lockCancel := context.WithTimeout(context.Background(), time.Second)
	defer lockCancel()
	lease, err := srv.cache.(*sharedcache.LockedCache).Coordinator.Acquire(lockCtx, key.String())
	if err != nil {
		t.Fatalf("cancellation retained miss leadership: %v", err)
	}
	_ = lease.Close()
}
