package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestProductionRepositoryRejectsWritesBeforeMirrorAuthentication(t *testing.T) {
	var reads, writes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "mirrorwriter" || password != "write-secret" {
			t.Error("configured mirror credentials missing on an allowed read")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writes.Add(1)
			w.WriteHeader(http.StatusNoContent) // Simulate a writable repository.
			return
		}
		reads.Add(1)
		w.Header().Set("Content-Length", "8")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "original")
		}
	}))
	defer upstream.Close()
	mirror, err := url.Parse(upstream.URL + "/ubuntu/")
	if err != nil {
		t.Fatal(err)
	}
	mirror.User = url.UserPassword("mirrorwriter", "write-secret")
	srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: mirror.String()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPost, http.MethodPatch, http.MethodOptions, http.MethodTrace} {
		// The download client has no credentials. Previously the proxy added
		// the mirror's writer credentials and forwarded these requests.
		res, err := srv.app.Test(httptest.NewRequest(method, "/ubuntu/pool/mutation.deb", strings.NewReader("untrusted replacement")))
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") != "GET, HEAD" || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("method=%s status=%d headers=%v", method, res.StatusCode, res.Header)
		}
	}
	if writes.Load() != 0 || reads.Load() != 0 || srv.cache.Stats().ItemCount != 0 {
		t.Fatalf("rejected request reached upstream/cache: reads=%d writes=%d items=%d", reads.Load(), writes.Load(), srv.cache.Stats().ItemCount)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		res, err := srv.app.Test(httptest.NewRequest(method, "/ubuntu/pool/mutation.deb", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil || res.StatusCode != http.StatusOK || res.Header.Get("Content-Length") != "8" || (method == http.MethodGet && string(body) != "original") || (method == http.MethodHead && len(body) != 0) {
			t.Fatalf("allowed %s: status=%d body=%q err=%v headers=%v", method, res.StatusCode, body, readErr, res.Header)
		}
	}
	if reads.Load() != 2 || writes.Load() != 0 {
		t.Fatalf("read forwarding changed: reads=%d writes=%d", reads.Load(), writes.Load())
	}
	// Management API handlers retain their own method/authentication rules.
	res, err := srv.app.Test(httptest.NewRequest(http.MethodPost, "/api/cache/purge", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("management API passed through repository method gate: status=%d", res.StatusCode)
	}
}
