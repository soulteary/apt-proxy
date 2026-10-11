package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/storage"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestDamagedPackageIsDownloadedAgain(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", "8")
		_, _ = w.Write([]byte("complete"))
	}))
	defer upstream.Close()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default()})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	backend, err := httpcache.NewDiskCacheWithConfig(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := httpcache.NewHandlerWithOptions(&storage.VerifiedCache{ExtendedCache: backend}, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	get := func(directive string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://proxy.example/ubuntu/pool/package.deb", nil)
		if directive != "" {
			req.Header.Set("Cache-Control", directive)
		}
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)
		return rec
	}
	get("")
	deadline := time.Now().Add(2 * time.Second)
	for backend.Stats().ItemCount == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if backend.Stats().ItemCount == 0 {
		t.Fatal("package not stored")
	}
	err = filepath.WalkDir(filepath.Join(dir, "body"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(path, "v1") {
			return os.Truncate(path, 3)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := get("only-if-cached"); rec.Code != 504 || hits.Load() != 1 {
		t.Fatalf("damaged cache-only read: %d hits=%d", rec.Code, hits.Load())
	}
	rec := get("")
	if rec.Code != 200 || rec.Body.String() != "complete" || hits.Load() != 2 {
		t.Fatalf("damaged object not repaired: %d %q hits=%d", rec.Code, rec.Body.String(), hits.Load())
	}
	// Cold miss followers wait for publication, so the next normal read must
	// receive either that complete cached object or a complete upstream object.
	rec = get("")
	if rec.Code != 200 || rec.Body.String() != "complete" {
		t.Fatal("repair did not survive the next read")
	}
}
