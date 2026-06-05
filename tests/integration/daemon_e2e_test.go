//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/cli"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// freeListenPort returns a "host:port" address that is currently free.
// There is an inherent race window between Close and the daemon Listen call,
// but it's small and acceptable for an integration test.
func freeListenPort(t *testing.T) (string, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	host, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return host, port
}

func waitForServer(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s within %s", url, timeout)
}

// TestDaemonE2E spins up cli.Daemon in-process on a random port and verifies
// that /healthz responds and that SIGTERM triggers a clean shutdown.
//
// Note: the daemon installs SIGTERM handlers on the global process. Tests
// that send SIGTERM cannot run in parallel with other tests in this binary;
// they're gated behind the `integration` build tag for that reason.
func TestDaemonE2E(t *testing.T) {
	host, port := freeListenPort(t)

	cacheDir := t.TempDir()
	cfg := &config.Config{
		Listen:   net.JoinHostPort(host, port),
		Mode:     distro.TypeAllDistros,
		Debug:    false,
		CacheDir: cacheDir,
		Cache: config.CacheConfig{
			MaxSizeGB:          1,
			TTLHours:           1,
			CleanupIntervalMin: 60,
		},
		Mirrors: config.MirrorConfig{
			Ubuntu: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu/",
		},
		TLS: config.TLSConfig{Enabled: false},
		Security: config.SecurityConfig{
			EnableAPIAuth:         false,
			APIRateLimitPerMinute: 0,
		},
		UpstreamKeepAlive: true,
	}

	// Sanity: cache dir must exist
	if _, err := os.Stat(cacheDir); err != nil {
		t.Fatalf("cache dir missing: %v", err)
	}

	if err := cli.ValidateConfig(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	// Run Daemon in goroutine; it blocks until SIGINT/SIGTERM.
	done := make(chan error, 1)
	go func() {
		done <- cli.Daemon(cfg)
	}()

	baseURL := fmt.Sprintf("http://%s:%s", host, port)
	waitForServer(t, baseURL+"/healthz", 5*time.Second)

	// /healthz should report ok (status 200) - we only assert reachability above.

	// Trigger graceful shutdown via SIGTERM to our own process.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("daemon did not exit within 15s after SIGTERM")
	}
}

// TestDaemonE2EHealthCheckPersists ensures that the cache directory is
// created and that healthz responds before any client request hits an
// upstream — useful as a smoke test in CI before SIGTERM.
func TestDaemonE2EHealthCheckPersists(t *testing.T) {
	host, port := freeListenPort(t)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	cfg := &config.Config{
		Listen:   net.JoinHostPort(host, port),
		Mode:     distro.TypeAllDistros,
		CacheDir: cacheDir,
		Cache: config.CacheConfig{
			MaxSizeGB:          1,
			TTLHours:           1,
			CleanupIntervalMin: 60,
		},
		UpstreamKeepAlive: true,
	}
	if err := cli.ValidateConfig(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cli.Daemon(cfg) }()

	baseURL := fmt.Sprintf("http://%s:%s", host, port)
	waitForServer(t, baseURL+"/healthz", 5*time.Second)

	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("daemon did not exit within 15s after SIGTERM")
	}
}
