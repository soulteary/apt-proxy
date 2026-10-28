package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/apt-proxy/internal/config"
	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	logger "github.com/soulteary/logger-kit/v3"
)

type reloadObserver struct{ completed chan struct{} }

func (w reloadObserver) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "configuration reload complete") {
		w.completed <- struct{}{}
	}
	return len(p), nil
}

func TestServeReloadBurstAndCancellation(t *testing.T) {
	s := makeIsolationServer(t, "lifecycle", "test-key", "http://127.0.0.1:1")
	observed := reloadObserver{completed: make(chan struct{}, 4)}
	s.log = logger.New(logger.Config{Output: observed, Format: logger.FormatJSON})
	ready := make(chan string, 1)
	s.app.Hooks().OnListen(func(d fiber.ListenData) error { ready <- net.JoinHostPort(d.Host, d.Port); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal)
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, signals) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("listener did not stop")
		}
	}()
	var addr string
	select {
	case addr = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not start")
	}
	client := &http.Client{Timeout: time.Second}
	res, err := client.Get("http://" + addr + "/version")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("version status=%d", res.StatusCode)
	}
	// Two events received while the debounce timer is active coalesce into one
	// follow-up reload, rather than overlapping registry publication.
	for i := 0; i < 2; i++ {
		select {
		case signals <- syscall.SIGHUP:
		case err := <-done:
			done <- err
			t.Fatalf("listener exited before reload: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("listener did not accept reload")
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-observed.completed:
		case <-time.After(5 * time.Second):
			t.Fatalf("reload %d never completed", i+1)
		}
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
		done <- nil
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish shutdown")
	}
}

func TestStartReturnsListenerErrors(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "tls"}[tls], func(t *testing.T) {
			s := makeIsolationServer(t, "invalid-listener", "key", "http://127.0.0.1:1")
			s.config.Listen = "127.0.0.1:invalid"
			s.config.TLS.Enabled = tls
			s.config.TLS.CertFile = "missing-cert.pem"
			s.config.TLS.KeyFile = "missing-key.pem"
			err := s.Start()
			var appErr *apperrors.AppError
			if !errors.As(err, &appErr) || appErr.Code != apperrors.ErrInternal {
				t.Fatalf("listener error=%v", err)
			}
		})
	}
}

func TestDaemonRejectsInvalidAndUninitializableConfigurations(t *testing.T) {
	if !errors.Is(Daemon(nil), errNilConfig) {
		t.Fatal("nil configuration accepted")
	}
	if err := Daemon(&config.Config{}); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: 1})
	cfg.Storage = config.StorageConfig{Backend: config.StorageBackendS3, S3: config.S3Config{Endpoint: "://invalid", Bucket: "test"}}
	err := Daemon(cfg)
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.ErrServerInit {
		t.Fatalf("initialization error=%v", err)
	}
}
