package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestProductionKnownLengthAdmissionOverload(t *testing.T) {
	headers, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseBody := func() { releaseOnce.Do(func() { close(release) }) }
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", "8")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(headers)
		<-release
		_, _ = io.WriteString(w, "complete")
	}))
	defer upstream.Close()
	srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}, Resources: config.ResourceConfig{MaxActiveDownloads: 1, MinFreeMB: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.shutdown() }()
	defer releaseBody()
	done := make(chan error, 1)
	go func() {
		res, err := srv.app.Test(httptest.NewRequest("GET", "/ubuntu/pool/one.deb", nil))
		if err == nil {
			_, err = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
		done <- err
	}()
	select {
	case <-headers:
	case <-time.After(5 * time.Second):
		t.Fatal("first upstream did not reach headers")
	}
	res, err := srv.app.Test(httptest.NewRequest("GET", "/ubuntu/pool/two.deb", nil))
	if err != nil {
		releaseBody()
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 503 || res.Header.Get("Retry-After") != "1" || calls.Load() != 1 {
		releaseBody()
		t.Fatalf("overload reached upstream: status=%d calls=%d headers=%v", res.StatusCode, calls.Load(), res.Header)
	}
	releaseBody()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted download did not finish")
	}
	httpcache.Writes.Wait()
	if stats := srv.resources.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
		t.Fatalf("completed download retained resources: %+v", stats)
	}
}
func TestProductionUnifiedCopiesPreserveResponseWhenCacheRefused(t *testing.T) {
	payload := bytes.Repeat([]byte("p"), 700<<10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "716800")
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()
	srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}, Resources: config.ResourceConfig{MaxObjectMB: 1, TotalTempMB: 1, MinFreeMB: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.shutdown() }()
	res, err := srv.app.Test(httptest.NewRequest("GET", "/ubuntu/pool/copies.deb", nil))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	httpcache.Writes.Wait()
	if err != nil || res.StatusCode != 200 || !bytes.Equal(got, payload) {
		t.Fatalf("cache refusal truncated response: status=%d bytes=%d err=%v", res.StatusCode, len(got), err)
	}
	if stats := srv.resources.Stats(); stats.TemporaryBytes != 0 || stats.ActiveDownloads != 0 || stats.PeakTemporaryBytes > 1<<20 {
		t.Fatalf("unified budget bypass=%+v", stats)
	}
	if srv.cache.Stats().ItemCount != 0 {
		t.Fatal("two overlapping copies exceeded quota but cache entry was admitted")
	}
}
