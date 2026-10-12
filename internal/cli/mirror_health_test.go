package cli

import (
	"encoding/json"
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
)

func TestProductionRuntimeMirrorFailoverAndStatus(t *testing.T) {
	var failed atomic.Bool
	var received atomic.Value
	var secondaryPackages atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(r.Header.Clone())
		if failed.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(w, "same signed metadata")
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive/dists/jammy/InRelease" {
			_, _ = io.WriteString(w, "same signed metadata")
			return
		}
		secondaryPackages.Add(1)
		_, _ = io.WriteString(w, "complete package")
	}))
	defer secondary.Close()
	cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
		Security:     config.SecurityConfig{EnableAPIAuth: true, APIKey: "status-key"},
		MirrorHealth: &mirrorhealth.Config{Enabled: true, FailureThreshold: 1, Repositories: []mirrorhealth.Repository{{ID: "ubuntu", Mirrors: []string{primary.URL + "/ubuntu/", secondary.URL + "/archive/"}}}},
	})
	cfg.Mirrors.Ubuntu = primary.URL + "/ubuntu/"
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	get := func(path string, auth bool) (int, []byte) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		if auth {
			req.Header.Set("X-API-Key", "status-key")
		}
		res, err := srv.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, body
	}
	if status, _ := get("/ubuntu/dists/jammy/InRelease", false); status != 200 {
		t.Fatal(status)
	}
	if state := srv.proxy.MirrorHealthStatus(); state.Repositories[0].SnapshotCount != 1 {
		t.Fatalf("metadata not observed: %+v upstream headers=%v", state, received.Load())
	}
	failed.Store(true)
	if status, body := get("/ubuntu/pool/test.deb", false); status != 200 || string(body) != "complete package" || secondaryPackages.Load() != 1 {
		t.Fatalf("runtime failover through cache/router failed: %d %q", status, body)
	}
	if status, _ := get("/api/mirrors/status", false); status != http.StatusUnauthorized {
		t.Fatal("status bypassed configured API authentication")
	}
	status, body := get("/api/mirrors/status", true)
	var health mirrorhealth.State
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatal(err)
	}
	if status != 200 || !health.Enabled || health.Repositories[0].Active != secondary.URL+"/archive/" || health.Repositories[0].LastDecision != "switched" {
		t.Fatalf("status missing selection: %d %+v", status, health)
	}
}
