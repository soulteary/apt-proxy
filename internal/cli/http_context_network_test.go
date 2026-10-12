//go:build unix

package cli

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestRepositoryStagingTrailerSurvivesActualFiberNetwork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		outcome string
	}{{"admitted", 64 << 10, "ADMITTED"}, {"refused", 2 << 20, "SKIP"}} {
		t.Run(tc.name, func(t *testing.T) {
			body := bytes.Repeat([]byte("b"), tc.size)
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Cache-Control", "public, max-age=3600")
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				w.Header().Set("ETag", `"body"`)
				_, _ = w.Write(body)
			})
			origin := httptest.NewServer(handler)
			candidate := httptest.NewServer(handler)
			defer origin.Close()
			defer candidate.Close()
			base := origin.URL + "/ubuntu/"
			bases := []string{base, candidate.URL + "/ubuntu/"}
			cfg := withTestMirrors(&config.Config{CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeUbuntu,
				Resources:    config.ResourceConfig{MaxObjectMB: 3, TotalTempMB: 1, MaxActiveDownloads: 1, MinFreeMB: 1},
				SharedCache:  &sharedcache.Config{CoordinationDir: t.TempDir(), Repositories: []sharedcache.Repository{{ID: "public", Bases: bases}}},
				MirrorHealth: &mirrorhealth.Config{Enabled: true, FailureThreshold: 1, Repositories: []mirrorhealth.Repository{{ID: "ubuntu", Mirrors: bases}}},
			})
			cfg.Mirrors.Ubuntu = base
			srv, err := NewServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- srv.app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
			defer func() {
				_ = srv.shutdown()
				if err := <-done; err != nil {
					t.Errorf("server exit:%v", err)
				}
			}()
			client := &http.Client{Timeout: 5 * time.Second}
			for attempt := 0; attempt < 2; attempt++ {
				res, err := client.Get("http://" + listener.Addr().String() + "/ubuntu/dists/test/main/binary-amd64/Packages")
				if err != nil {
					t.Fatal(err)
				}
				wantLabel, wantTrailer := "MISS", tc.outcome
				if tc.outcome == "ADMITTED" && attempt == 1 {
					wantLabel, wantTrailer = "HIT", ""
				}
				assertRepositoryStagingResponse(t, srv, res, body, wantLabel, wantTrailer, tc.outcome == "SKIP")
			}
			wantCalls := int32(2)
			if tc.outcome == "ADMITTED" {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatalf("origin calls=%d want=%d", calls.Load(), wantCalls)
			}
		})
	}
}

func assertRepositoryStagingResponse(t *testing.T, srv *Server, res *http.Response, body []byte, wantLabel, wantTrailer string, allowSkip bool) {
	t.Helper()
	got, readErr := io.ReadAll(res.Body)
	_ = res.Body.Close()
	httpcache.Writes.Wait()
	if readErr != nil || res.StatusCode != http.StatusOK || !bytes.Equal(got, body) || res.ProtoMajor != 1 || res.ProtoMinor != 1 {
		t.Fatalf("delivery status=%d proto=%s bytes=%d err=%v", res.StatusCode, res.Proto, len(got), readErr)
	}
	validLabel := res.Header.Get("X-Cache") == wantLabel
	// A buffered Fiber adaptor can preserve final SKIP as an initial header;
	// a streamed writer retains MISS plus the final trailer.
	if allowSkip && res.Header.Get("X-Cache") == "SKIP" {
		validLabel = true
	}
	if !validLabel || res.Trailer.Get("X-Cache-Staging") != wantTrailer {
		t.Fatalf("Fiber outcome: headers=%v trailers=%v, want %s/%s", res.Header, res.Trailer, wantLabel, wantTrailer)
	}
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
		t.Fatalf("resource ownership:%+v", stats)
	}
}
