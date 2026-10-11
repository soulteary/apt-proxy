package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/storage"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
)

func downloadTestServer(t *testing.T, upstream string) (*httptest.Server, httpcache.ExtendedCache) {
	t.Helper()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream+"/ubuntu/")
	ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default()})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := httpcache.NewHandlerWithOptions(&storage.VerifiedCache{ExtendedCache: backend}, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached
	server := httptest.NewServer(ps)
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	return server, backend
}

func TestInterruptedUpstreamDownloadRecovers(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprint("chunked=", chunked), func(t *testing.T) {
			var hits atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if hits.Add(1) == 1 {
					conn, buf, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					if chunked {
						fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n")
					} else {
						fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Length: 8\r\n\r\nabc")
					}
					buf.Flush()
					return
				}
				w.Header().Set("Content-Length", "8")
				io.WriteString(w, "complete")
			}))
			defer upstream.Close()
			server, cache := downloadTestServer(t, upstream.URL)
			client := &http.Client{Timeout: 3 * time.Second}
			url := server.URL + "/ubuntu/pool/interrupted.deb"
			res, err := client.Get(url)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			if chunked {
				if res.StatusCode != 502 {
					t.Fatalf("incomplete chunked body: status=%d body=%q", res.StatusCode, body)
				}
			} else if readErr == nil {
				t.Fatal("truncated fixed-length response appeared complete")
			}
			if cache.Stats().ItemCount != 0 {
				t.Fatal("incomplete download cached")
			}
			res, err = client.Get(url)
			if err != nil {
				t.Fatal(err)
			}
			body, err = io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || string(body) != "complete" {
				t.Fatalf("recovery: %d %q %v", res.StatusCode, body, err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for cache.Stats().ItemCount == 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			res, err = client.Get(url)
			if err != nil {
				t.Fatal(err)
			}
			body, err = io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || string(body) != "complete" || hits.Load() != 2 {
				t.Fatalf("replacement cache: %q err=%v hits=%d", body, err, hits.Load())
			}
		})
	}
}

func TestCanceledColdLeaderDoesNotStrandFollowers(t *testing.T) {
	entered := make(chan struct{})
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8")
		if hits.Add(1) == 1 {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			close(entered)
			<-r.Context().Done()
			return
		}
		io.WriteString(w, "complete")
	}))
	defer upstream.Close()
	server, _ := downloadTestServer(t, upstream.URL)
	client := &http.Client{Timeout: 3 * time.Second}
	url := server.URL + "/ubuntu/pool/canceled.deb"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("leader did not reach upstream")
	}
	follower := make(chan error, 1)
	go func() {
		res, err := client.Get(url)
		if err == nil {
			var b []byte
			b, err = io.ReadAll(res.Body)
			res.Body.Close()
			if err == nil && (res.StatusCode != 200 || string(b) != "complete") {
				err = fmt.Errorf("incomplete follower: %d %q", res.StatusCode, b)
			}
		}
		follower <- err
	}()
	cancel()
	select {
	case err := <-follower:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("follower stranded after leader cancellation")
	}
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("leader stranded")
	}
	if hits.Load() != 2 {
		t.Fatalf("recovery downloads=%d, want 2", hits.Load())
	}
}
