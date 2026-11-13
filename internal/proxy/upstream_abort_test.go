// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/resource"
)

// budgetedProxyServer assembles the daemon's handler order behind a real
// net/http server: routing, a cache with a staging budget, the reverse proxy.
// With a staging budget the cache defers response headers and re-frames a
// cacheable miss as chunked with a trailer, which is the production framing.
func budgetedProxyServer(t *testing.T, configure func(*Options)) (*httptest.Server, *resource.Manager) {
	t.Helper()
	budget, err := resource.New(resource.Config{MaxObjectBytes: 1 << 20, TotalTempBytes: 4 << 20, MaxActiveDownloads: 4})
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{State: newTestState(), Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default(), Resources: budget}
	if configure != nil {
		configure(&opts)
	}
	ps, err := NewPackageStruct(opts)
	if err != nil {
		t.Fatal(err)
	}
	cache := httpcache.NewMemoryCacheWithConfig(httpcache.DefaultCacheConfig().WithCleanupInterval(0))
	cached := httpcache.NewHandlerWithOptions(cache, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default(), StagingBudget: budget, TempDir: t.TempDir()})
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
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	return server, budget
}

// fetched is what a client received. status is zero when the connection
// failed before any response header arrived.
type fetched struct {
	status        int
	contentLength int64
	body          []byte
}

// fetch performs a GET. The error is set when the request or the body read
// fails, which is how a client observes an aborted connection.
func fetch(url string) (fetched, error) {
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(url)
	if err != nil {
		return fetched{}, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	return fetched{status: res.StatusCode, contentLength: res.ContentLength, body: body}, err
}

// A fixed-length upstream response that ends early must not reach the client
// as a complete response, also when the cache has replaced Content-Length
// with chunked framing.
func TestTruncatedUpstreamIsVisibleThroughStagedFraming(t *testing.T) {
	// Large enough that the proxy has sent headers and part of the body before
	// the upstream connection ends.
	const declared, sent = 256 << 10, 128 << 10
	full := bytes.Repeat([]byte("p"), declared)
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nCache-Control: max-age=60\r\n\r\n", declared)
			_, _ = buf.Write(full[:sent])
			_ = buf.Flush()
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(declared))
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = w.Write(full)
	}))
	defer upstream.Close()
	server, budget := budgetedProxyServer(t, func(o *Options) {
		o.State.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	})
	url := server.URL + "/ubuntu/pool/truncated.deb"

	got, err := fetch(url)
	if got.status != http.StatusOK || got.contentLength != -1 {
		t.Fatalf("expected the staged response without Content-Length: status=%d length=%d err=%v", got.status, got.contentLength, err)
	}
	if err == nil {
		t.Fatalf("truncated upstream body (%d of %d bytes) reached the client as a complete response", len(got.body), declared)
	}

	got, err = fetch(url)
	if err != nil || got.status != http.StatusOK || !bytes.Equal(got.body, full) {
		t.Fatalf("recovery: status=%d bytes=%d err=%v", got.status, len(got.body), err)
	}
	httpcache.Writes.Wait()
	if stats := budget.Stats(); stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
		t.Fatalf("resources retained after an aborted download: %+v", stats)
	}
}

// The Go client accepts any three-digit status. One that net/http cannot
// write must become a 502 instead of a panic in WriteHeader.
func TestInvalidUpstreamStatusBecomesBadGateway(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = fmt.Fprint(buf, "HTTP/1.1 099 Out Of Range\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			_ = buf.Flush()
			return
		}
		_, _ = io.WriteString(w, "package")
	}))
	defer upstream.Close()
	server, budget := budgetedProxyServer(t, func(o *Options) {
		o.State.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	})
	url := server.URL + "/ubuntu/pool/status.deb"

	got, err := fetch(url)
	if err != nil || got.status != http.StatusBadGateway {
		t.Fatalf("invalid upstream status: status=%d err=%v", got.status, err)
	}
	got, err = fetch(url)
	if err != nil || got.status != http.StatusOK || string(got.body) != "package" {
		t.Fatalf("follow-up request: status=%d body=%q err=%v", got.status, got.body, err)
	}
	httpcache.Writes.Wait()
	if stats := budget.Stats(); stats.ActiveDownloads != 0 {
		t.Fatalf("download slot retained: %+v", stats)
	}
}

type panicAfterBody struct {
	data  io.Reader
	value any
}

func (b *panicAfterBody) Read(p []byte) (int, error) {
	n, err := b.data.Read(p)
	if err == io.EOF {
		panic(b.value)
	}
	return n, err
}

func (*panicAfterBody) Close() error { return nil }

// lockedBuffer is a log sink that can be read while other goroutines still
// write to it. The cache stores a response in the background and logs a failed
// store after the request has been answered.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// A defect inside the cache's worker goroutine fails one request. It must not
// end the process, and a response that already started must not look complete.
func TestWorkerPanicFailsOnlyItsRequest(t *testing.T) {
	previous := logger.Default()
	var logs lockedBuffer
	logger.SetDefault(logger.New(logger.Config{Level: logger.InfoLevel, Output: &logs, Format: logger.FormatJSON}))
	t.Cleanup(func() { logger.SetDefault(previous) })

	server, _ := budgetedProxyServer(t, func(o *Options) {
		o.TransportOverride = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/before.deb"):
				panic("defect before the response")
			case strings.HasSuffix(r.URL.Path, "/during.deb"):
				return &http.Response{
					StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1, ContentLength: 8,
					Header:  http.Header{"Content-Length": {"8"}, "Cache-Control": {"max-age=60"}},
					Body:    &panicAfterBody{data: strings.NewReader("abc"), value: "defect during the response"},
					Request: r,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1, ContentLength: 2,
				Header: http.Header{"Content-Length": {"2"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: r,
			}, nil
		})
	})

	got, err := fetch(server.URL + "/ubuntu/pool/before.deb")
	if err != nil || got.status != http.StatusBadGateway {
		t.Fatalf("panic before the response: status=%d err=%v", got.status, err)
	}

	if got, err := fetch(server.URL + "/ubuntu/pool/during.deb"); err == nil {
		t.Fatalf("panic during the response delivered %q as complete", got.body)
	}

	got, err = fetch(server.URL + "/ubuntu/pool/after.deb")
	if err != nil || got.status != http.StatusOK || string(got.body) != "ok" {
		t.Fatalf("server did not keep serving: status=%d body=%q err=%v", got.status, got.body, err)
	}
	// The client may retry a request whose connection closed without a reply,
	// so each defect is reported at least once.
	for _, value := range []string{"defect before the response", "defect during the response"} {
		if !strings.Contains(logs.String(), value) {
			t.Fatalf("panic %q was not reported", value)
		}
	}
}

func TestUpstreamAbortTracking(t *testing.T) {
	t.Run("only a served request propagates", func(t *testing.T) {
		ctx, abort := trackUpstreamAbort(context.Background())
		markUpstreamAborted(ctx)
		abort.propagate() // not served by net/http: nothing recovers a panic here

		served := context.WithValue(context.Background(), http.ServerContextKey, &http.Server{})
		ctx, abort = trackUpstreamAbort(served)
		abort.propagate() // nothing failed
		markUpstreamAborted(ctx)
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Fatalf("recovered %v, want http.ErrAbortHandler", recovered)
			}
		}()
		abort.propagate()
	})

	t.Run("untracked context", func(t *testing.T) {
		markUpstreamAborted(context.Background()) // must not panic
	})

	t.Run("body errors mark the request, EOF does not", func(t *testing.T) {
		ctx, abort := trackUpstreamAbort(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)

		complete := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}
		trackUpstreamBody(complete)
		if _, err := io.ReadAll(complete.Body); err != nil || abort.failed.Load() {
			t.Fatalf("complete body: err=%v aborted=%v", err, abort.failed.Load())
		}

		broken := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotestErrReader{errors.New("reset")}), Request: req}
		trackUpstreamBody(broken)
		if _, err := io.ReadAll(broken.Body); err == nil || !abort.failed.Load() {
			t.Fatalf("broken body: err=%v aborted=%v", err, abort.failed.Load())
		}
	})

	t.Run("bodies the reverse proxy needs untouched", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		body := io.NopCloser(strings.NewReader(""))
		for _, resp := range []*http.Response{
			{StatusCode: http.StatusSwitchingProtocols, Body: body, Request: req},
			{StatusCode: http.StatusOK, Body: http.NoBody, Request: req},
			{StatusCode: http.StatusOK, Body: nil, Request: req},
			{StatusCode: http.StatusOK, Body: body},
		} {
			before := resp.Body
			trackUpstreamBody(resp)
			if resp.Body != before {
				t.Fatalf("status %d: body was wrapped", resp.StatusCode)
			}
		}
	})

	t.Run("status range", func(t *testing.T) {
		for code, want := range map[int]bool{0: false, 99: false, 100: true, 200: true, 599: true, 999: true, 1000: false} {
			if validUpstreamStatus(code) != want {
				t.Errorf("validUpstreamStatus(%d) = %v, want %v", code, !want, want)
			}
		}
	})
}

type iotestErrReader struct{ err error }

func (r iotestErrReader) Read([]byte) (int, error) { return 0, r.err }

// Routing rewrites the request it serves. The caller's request must stay as
// the client sent it, because the access log reads it after the handler ran.
func TestServeHTTPLeavesCallerRequestUnchanged(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer upstream.Close()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default()})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://archive.ubuntu.com/ubuntu/pool/main/a.deb?x=1", nil)
	req.Header.Set("X-Probe", "original")
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if req.URL.String() != "http://archive.ubuntu.com/ubuntu/pool/main/a.deb?x=1" || req.Host != "archive.ubuntu.com" || req.Header.Get("X-Probe") != "original" {
		t.Fatalf("caller's request was rewritten: url=%s host=%s", req.URL, req.Host)
	}
}
