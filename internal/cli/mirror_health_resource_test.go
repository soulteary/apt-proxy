package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestProductionFailoverSharesAdmissionAndVerifiesCandidateCache(t *testing.T) {
	const manifest = "same repository manifest"
	const original, updated = "package-original", "package-updated!"
	var primaryPackages, candidateProbes, candidatePackages atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ubuntu/dists/jammy/InRelease" {
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = io.WriteString(w, manifest)
			return
		}
		primaryPackages.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releasePackage := func() { releaseOnce.Do(func() { close(release) }) }
	candidate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/dists/jammy/InRelease" {
			candidateProbes.Add(1)
			_, _ = io.WriteString(w, manifest)
			return
		}
		payload := updated
		if candidatePackages.Add(1) == 1 {
			close(entered)
			<-release
			payload = original
		}
		w.Header().Set("Content-Length", "16")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = io.WriteString(w, payload)
	}))
	defer candidate.Close()
	defer releasePackage()
	dir := t.TempDir()
	cfg := withTestMirrors(&config.Config{CacheDir: dir, Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
		Resources: config.ResourceConfig{MaxObjectMB: 1, TotalTempMB: 1, MaxActiveDownloads: 1, MinFreeMB: 1},
		MirrorHealth: &mirrorhealth.Config{Enabled: true, FailureThreshold: 1,
			Repositories: []mirrorhealth.Repository{{ID: "ubuntu", Mirrors: []string{primary.URL + "/ubuntu/", candidate.URL + "/archive/"}}}},
	})
	cfg.Mirrors.Ubuntu = primary.URL + "/ubuntu/"
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { releasePackage(); _ = srv.shutdown() }()
	type result struct {
		status                  int
		body, cache, retryAfter string
		err                     error
	}
	get := func(path string) result {
		res, err := srv.app.Test(httptest.NewRequest("GET", path, nil), fiber.TestConfig{Timeout: 5 * time.Second})
		if err != nil {
			return result{err: err}
		}
		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		return result{status: res.StatusCode, body: string(body), cache: res.Header.Get("X-Cache"), retryAfter: res.Header.Get("Retry-After"), err: readErr}
	}
	if res := get("/ubuntu/dists/jammy/InRelease"); res.err != nil || res.status != 200 || res.body != manifest {
		t.Fatalf("manifest observation failed: %+v", res)
	}
	done := make(chan result, 1)
	var request sync.WaitGroup
	request.Add(1)
	go func() { defer request.Done(); done <- get("/ubuntu/pool/integrated.deb") }()
	defer func() { releasePackage(); request.Wait() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("failover did not reach the candidate package")
	}
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 1 {
		t.Fatalf("failover lost its download admission: %+v", stats)
	}
	primaryBeforeOverload := primaryPackages.Load()
	if primaryBeforeOverload == 0 {
		t.Fatal("failover did not attempt the primary mirror")
	}
	if res := get("/ubuntu/pool/overloaded.deb"); res.err != nil || res.status != 503 || res.retryAfter != "1" || res.body != "" {
		t.Fatalf("parallel failover bypassed admission: %+v", res)
	}
	if primaryPackages.Load() != primaryBeforeOverload || candidateProbes.Load() != 1 || candidatePackages.Load() != 1 {
		t.Fatalf("overload reached a mirror: primary=%d probes=%d candidate=%d", primaryPackages.Load(), candidateProbes.Load(), candidatePackages.Load())
	}
	releasePackage()
	select {
	case res := <-done:
		if res.err != nil || res.status != 200 || res.body != original || res.cache != "MISS" {
			t.Fatalf("candidate response failed: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("candidate response did not finish")
	}
	httpcache.Writes.Wait()
	paths, err := filepath.Glob(filepath.Join(dir, "body", "v1", "*"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("candidate body entries=%v err=%v", paths, err)
	}
	if err := os.WriteFile(paths[0], bytes.Repeat([]byte("x"), len(original)), 0600); err != nil {
		t.Fatal(err)
	}
	if res := get("/ubuntu/pool/integrated.deb"); res.err != nil || res.status != 200 || res.body != updated || res.cache != "MISS" {
		t.Fatalf("candidate corruption did not refetch: %+v", res)
	}
	httpcache.Writes.Wait()
	if res := get("/ubuntu/pool/integrated.deb"); res.err != nil || res.status != 200 || res.body != updated || res.cache != "HIT" {
		t.Fatalf("verified candidate entry was not reusable: %+v", res)
	}
	if primaryPackages.Load() != primaryBeforeOverload || candidateProbes.Load() != 1 || candidatePackages.Load() != 2 {
		t.Fatalf("candidate reuse changed route or repeated probes: primary=%d probes=%d candidate=%d", primaryPackages.Load(), candidateProbes.Load(), candidatePackages.Load())
	}
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 || stats.PeakTemporaryBytes > 1<<20 {
		t.Fatalf("integrated requests retained or exceeded resources: %+v", stats)
	}
}
