package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestLoadedCacheLimitsReachBackend(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		args       []string
		envZero    bool
		want       config.CacheConfig
	}{
		{name: "defaults", yaml: "{}\n", want: config.CacheConfig{MaxSize: httpcache.DefaultMaxCacheSize, TTL: httpcache.DefaultCacheTTL, CleanupInterval: httpcache.DefaultCleanupInterval}},
		{name: "yaml-positive", yaml: "cache:\n  max_size_gb: 2\n  ttl_hours: 48\n  cleanup_interval_min: 30\n", want: config.CacheConfig{MaxSize: 2 << 30, TTL: 48 * time.Hour, CleanupInterval: 30 * time.Minute}},
		{name: "cli-positive", yaml: "{}\n", args: []string{"--cache-max-size=2", "--cache-ttl=48", "--cache-cleanup-interval=30"}, want: config.CacheConfig{MaxSize: 2 << 30, TTL: 48 * time.Hour, CleanupInterval: 30 * time.Minute}},
		{name: "cli-zero", yaml: "cache:\n  max_size_gb: 2\n  ttl_hours: 48\n  cleanup_interval_min: 30\n", args: []string{"--cache-max-size=0", "--cache-ttl=0", "--cache-cleanup-interval=0"}},
		{name: "env-zero", yaml: "cache:\n  max_size_gb: 2\n  ttl_hours: 48\n  cleanup_interval_min: 30\n", envZero: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadCacheLimitFixture(t, tc.yaml, tc.args, tc.envZero)
			if cfg.Cache.MaxSize != tc.want.MaxSize || cfg.Cache.TTL != tc.want.TTL || cfg.Cache.CleanupInterval != tc.want.CleanupInterval {
				t.Fatalf("resolved config lost limit selection: got=%+v want=%+v", cfg.Cache, tc.want)
			}
			assertLoadedCacheLimits(t, cfg, tc.want)
		})
	}
}

func loadCacheLimitFixture(t *testing.T, yaml string, args []string, envZero bool) *config.Config {
	t.Helper()
	for _, name := range []string{config.EnvCacheMaxSize, config.EnvCacheTTL, config.EnvCacheCleanupInterval} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		if envZero {
			t.Setenv(name, "0")
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "apt-proxy.yaml")
	if err := os.WriteFile(file, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	prior := os.Args
	t.Cleanup(func() { os.Args = prior })
	os.Args = append([]string{"apt-proxy", "--config=" + file, "--cachedir=" + filepath.Join(dir, "cache"), "--mode=ubuntu", "--host=127.0.0.1", "--port=0"}, args...)
	cfg, err := config.ParseFlagsWithConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func assertLoadedCacheLimits(t *testing.T, cfg *config.Config, want config.CacheConfig) {
	t.Helper()
	var now atomic.Int64
	now.Store(time.Now().UTC().UnixNano())
	oldClock := httpcache.Clock
	httpcache.Clock = func() time.Time { return time.Unix(0, now.Load()).UTC() }
	t.Cleanup(func() { httpcache.Writes.Wait(); httpcache.Clock = oldClock })
	srv, err := NewServer(withTestMirrors(cfg))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	actual := srv.buildCacheConfig()
	if actual.MaxSize != want.MaxSize || actual.TTL != want.TTL || actual.CleanupInterval != want.CleanupInterval {
		t.Errorf("backend policy discarded resolved limits: got=%+v want=%+v", actual, want)
	}
	const key = "GET:http://mirror.example/package.deb"
	res := httpcache.NewResourceBytes(http.StatusOK, []byte("complete"), http.Header{"Content-Length": {"8"}})
	if err := srv.cache.Store(res, key); err != nil {
		t.Fatal(err)
	}
	_ = res.Close()
	now.Add(int64(8 * 24 * time.Hour))
	result := srv.cache.Cleanup()
	if want.TTL > 0 {
		if result.RemovedItems != 1 || srv.cache.Stats().ItemCount != 0 {
			t.Fatalf("enabled TTL failed to remove old entry: %+v", result)
		}
		return
	}
	if result.RemovedItems != 0 {
		t.Fatalf("disabled TTL evicted old entry: %+v", result)
	}
	found, err := srv.cache.Retrieve(key)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(found)
	_ = found.Close()
	if readErr != nil || string(body) != "complete" {
		t.Fatalf("disabled TTL lost old complete entry: %q %v", body, readErr)
	}
}

func TestCacheLimitBuilderKeepsNegativeFallback(t *testing.T) {
	srv := &Server{config: &config.Config{Cache: config.CacheConfig{MaxSize: -1, TTL: -1, CleanupInterval: -1}}}
	actual := srv.buildCacheConfig()
	if actual.MaxSize != httpcache.DefaultMaxCacheSize || actual.TTL != httpcache.DefaultCacheTTL || actual.CleanupInterval != httpcache.DefaultCleanupInterval {
		t.Fatalf("negative raw values changed their existing default fallback: %+v", actual)
	}
}
