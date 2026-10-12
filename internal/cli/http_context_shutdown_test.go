//go:build unix

package cli

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestRepositoryShutdownCancelsStalledOrigin(t *testing.T) {
	for _, method := range []string{"server", "fiber"} {
		t.Run(method, func(t *testing.T) {
			fixture := newRepositoryShutdownFixture(t)
			waitRepositoryEvent(t, fixture.started, "upstream request")
			var err error
			if method == "server" {
				err = fixture.server.shutdown()
			} else {
				err = fixture.server.app.ShutdownWithTimeout(time.Second)
			}
			if err != nil {
				t.Fatalf("%s shutdown exhausted its drain deadline: %v", method, err)
			}
			waitRepositoryEvent(t, fixture.canceled, "upstream cancellation")
			waitRepositoryEvent(t, fixture.clientDone, "proxy client completion")
			if stats := fixture.server.resources.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
				t.Fatalf("shutdown retained download resources: %+v", stats)
			}
		})
	}
}

type repositoryShutdownFixture struct {
	server     *Server
	started    <-chan struct{}
	canceled   <-chan struct{}
	clientDone <-chan struct{}
}

func newRepositoryShutdownFixture(t *testing.T) repositoryShutdownFixture {
	t.Helper()
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, "released")
		}
	}))
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); origin.Close() })
	cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu})
	cfg.Mirrors.Ubuntu = origin.URL + "/ubuntu/"
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		_ = server.shutdown()
		select {
		case err := <-serveDone:
			if err != nil {
				t.Errorf("server exit: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not exit")
		}
	})
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + listener.Addr().String() + "/ubuntu/dists/test/main/binary-amd64/Packages")
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
	}()
	return repositoryShutdownFixture{server: server, started: started, canceled: canceled, clientDone: clientDone}
}

func waitRepositoryEvent(t *testing.T, event <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatalf("did not observe %s", name)
	}
}
