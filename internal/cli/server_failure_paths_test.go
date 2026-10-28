package cli

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/metalink"
	"github.com/soulteary/apt-proxy/internal/passthrough"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
	health "github.com/soulteary/health-kit/v4"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	vfs "github.com/soulteary/vfs-kit"
)

func TestNewServerRejectsPoliciesBeforeInitialization(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"upstream", func(c *config.Config) { c.UpstreamAccess = &access.UpstreamConfig{AllowedPorts: []int{-1}} }},
		{"integrity", func(c *config.Config) { c.Cache.Integrity = "invalid" }},
		{"metalink", func(c *config.Config) { c.Metalink = &metalink.Config{PublicURL: "invalid"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir()})
			tt.mutate(cfg)
			if s, err := NewServer(cfg); err == nil || s != nil {
				t.Fatalf("server=%v error=%v", s, err)
			}
		})
	}
}

func TestInitializeReportsConfigurationAndBackendFailures(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	shared := func() *sharedcache.Config {
		return &sharedcache.Config{Repositories: []sharedcache.Repository{{ID: "public", Bases: []string{"http://archive.example/debian/"}}}}
	}
	for _, tt := range []struct {
		name   string
		mutate func(*config.Config)
		want   string
	}{
		{"auth", func(c *config.Config) { c.Security.EnableAPIAuth = true }, "no API key"},
		{"resource", func(c *config.Config) { c.Resources.MaxObjectMB = math.MaxInt64 }, "resource limits"},
		{"shared policy", func(c *config.Config) { c.SharedCache = &sharedcache.Config{} }, "explicit repositories"},
		{"S3 disk coordination", func(c *config.Config) {
			c.SharedCache = shared()
			c.SharedCache.CoordinationDir = t.TempDir()
			c.Storage.Backend = "s3"
		}, "requires disk"},
		{"coordination path", func(c *config.Config) {
			c.SharedCache = shared()
			c.SharedCache.CoordinationDir = filepath.Join(blocked, "coordination")
		}, "not a directory"},
		{"passthrough", func(c *config.Config) { c.Passthrough = []string{"http://user:password@example.com"} }, "passthrough"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: 1})
			tt.mutate(cfg)
			s := &Server{config: cfg}
			s.initLogger()
			err := s.initialize()
			if s.cache != nil {
				defer s.cache.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want %q", err, tt.want)
			}
		})
	}
}

func TestS3InitializationAndHealthCheck(t *testing.T) {
	var failed atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failed.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodHead && (r.URL.Path == "/testbucket/" || r.URL.Path == "/testbucket"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>testbucket</Name><IsTruncated>false</IsTruncated></ListBucketResult>`)
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected S3 request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer upstream.Close()
	cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: 1})
	cfg.Storage = config.StorageConfig{Backend: "s3", S3: config.S3Config{Endpoint: strings.TrimPrefix(upstream.URL, "http://"), Bucket: "testbucket", Region: "us-east-1", UsePathStyle: true, InlineMaxMB: 1, TempDir: t.TempDir()}}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown()
	result := s.healthAggregator.Check(context.Background())
	if result.Status != health.StatusHealthy {
		t.Fatalf("health=%+v", result)
	}
	failed.Store(true)
	result = s.healthAggregator.Check(context.Background())
	if result.Status == health.StatusHealthy {
		t.Fatalf("bucket access loss reported healthy: %+v", result)
	}
}

type closeFailureCache struct {
	httpcache.ExtendedCache
	err error
}

func (c closeFailureCache) Close() error { _ = c.ExtendedCache.Close(); return c.err }

func TestShutdownPreservesCacheCloseFailure(t *testing.T) {
	s := makeIsolationServer(t, "close-failure", "key", "http://127.0.0.1:1")
	sentinel := errors.New("cache close failed")
	s.cache = closeFailureCache{s.cache, sentinel}
	if err := s.shutdown(); !errors.Is(err, sentinel) {
		t.Fatalf("shutdown error=%v", err)
	}
}

func TestServerOperationalRoutesAndLogs(t *testing.T) {
	t.Setenv(config.EnvLogLevel, "")
	t.Setenv(config.EnvLogLevelLegacy, "debug")
	s := makeIsolationServer(t, "routes", "key", "http://127.0.0.1:1")
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/", 200}, {"POST", "/api/mirrors/health", 405}} {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.Header.Set("X-API-Key", "key")
		res, err := s.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tt.status {
			t.Fatalf("%s %s status=%d", tt.method, tt.path, res.StatusCode)
		}
	}
	list, err := passthrough.Parse([]string{"http://public.example:8080", "https://secure.example"})
	if err != nil {
		t.Fatal(err)
	}
	s.logPassthrough(list)
	s.registry = nil
	s.logRegisteredDistributions()
	vfs.LogCloseError(errors.New("fixture close failed"))
}

func TestHealthDetailsCanOmitChecks(t *testing.T) {
	cfg := health.DefaultConfig()
	cfg.IncludeDetails = true
	cfg.IncludeChecks = false
	agg := health.NewAggregator(cfg)
	app := fiber.New()
	app.Get("/healthz", fiberHealthHandler(agg))
	res, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(body), "\"checks\":{") {
		t.Fatalf("health=%d %s", res.StatusCode, body)
	}
}
