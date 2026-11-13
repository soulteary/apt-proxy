package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestMirrorRefreshAPIFailurePreservesRequests(t *testing.T) {
	for _, tc := range []struct {
		kind string
		// want is the status of the refresh that runs into the failure.
		want int
	}{
		{"invalid-yaml", http.StatusInternalServerError},
		// The mirror in service stops answering: its distribution keeps it.
		{"unreachable-mirror", http.StatusOK},
		// A distribution added by the reload has no mirror to keep.
		{"unreachable-new-distribution", http.StatusServiceUnavailable},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var fail atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if fail.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Cache-Control", "no-store")
				_, _ = io.WriteString(w, "repository")
			}))
			defer upstream.Close()
			path := filepath.Join(t.TempDir(), "distributions.yaml")
			valid := strings.ReplaceAll(overrideUbuntuYAML, "mirrors.example.com/ubuntu/", upstream.URL+"/ubuntu/")
			writeAt(t, path, valid)
			cfg := withTestMirrors(&config.Config{
				CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
				DistributionsConfigPath: path,
				Security:                config.SecurityConfig{EnableAPIAuth: true, APIKey: "refresh-key"},
			})
			cfg.Mirrors.Ubuntu = upstream.URL + "/ubuntu/"
			switch tc.kind {
			case "unreachable-mirror":
				cfg.Mirrors.Ubuntu = ""
			case "unreachable-new-distribution":
				// Custom distributions are only served in the all-distributions mode.
				cfg.Mode = distro.TypeAllDistros
			}
			srv, err := NewServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = srv.shutdown() })
			request := func(target string) {
				t.Helper()
				res, err := srv.app.Test(httptest.NewRequest(http.MethodGet, target, nil))
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(res.Body)
				res.Body.Close()
				if err != nil || res.StatusCode != 200 || string(body) != "repository" {
					t.Fatalf("working request %s: status=%d body=%q err=%v", target, res.StatusCode, body, err)
				}
			}
			get := func() {
				t.Helper()
				request("/ubuntu/pool/package.deb")
			}
			get()
			reloaded := valid
			switch tc.kind {
			case "invalid-yaml":
				reloaded = "distributions: ["
			case "unreachable-mirror":
				deadline := time.Now().Add(2 * time.Second)
				for {
					if _, ok := srv.proxy.BenchmarkEngine().Cache().GetCachedResult(distro.TypeUbuntu); ok {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("initial mirror election did not finish")
					}
					time.Sleep(time.Millisecond)
				}
				fail.Store(true)
			case "unreachable-new-distribution":
				deepin := strings.ReplaceAll(customDistroYAML, "community-packages.deepin.com/deepin/", upstream.URL+"/deepin/")
				reloaded = valid + strings.TrimPrefix(deepin, "\ndistributions:\n")
				fail.Store(true)
			}
			writeAt(t, path, reloaded)
			refresh := func(want int, success bool) {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, "/api/mirrors/refresh", nil)
				req.Header.Set("X-API-Key", "refresh-key")
				res, err := srv.app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var body map[string]any
				if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != want || (body["success"] == true) != success {
					t.Fatalf("refresh: status=%d want=%d body=%v", res.StatusCode, want, body)
				}
				if !success && body["code"] == nil {
					t.Fatalf("failed refresh omitted structured error: %v", body)
				}
			}
			refresh(tc.want, tc.want == http.StatusOK)
			fail.Store(false)
			get() // Old route still works without another successful election.
			if tc.kind == "invalid-yaml" {
				writeAt(t, path, valid)
			}
			refresh(http.StatusOK, true)
			get()
			if tc.kind == "unreachable-new-distribution" {
				request("/deepin/pool/package.deb")
			}
		})
	}
}
