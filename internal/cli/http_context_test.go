package cli

import (
	"context"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	"github.com/valyala/fasthttp"
)

type repositoryTraceKey struct{}

func TestRepositoryHTTPContextPreservesUserContextAfterRequestReuse(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), repositoryTraceKey{}, "original-trace"), deadline)
	defer cancel()
	lifecycle, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	var request fasthttp.RequestCtx
	request.Request.SetRequestURI("http://proxy.example/ubuntu/pool/package.deb")
	request.Request.Header.SetMethod(http.MethodGet)
	app := fiber.New()
	c := app.AcquireCtx(&request)
	c.SetContext(parent)
	var retained context.Context
	handler := repositoryHTTPHandler(lifecycle, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retained = r.Context()
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := handler(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	request.Request.Reset()
	request.SetUserValue(repositoryTraceKey{}, "another-request")
	if retained == nil || retained.Value(repositoryTraceKey{}) != "original-trace" {
		t.Fatalf("retained context borrowed a recycled request: %v", retained)
	}
	if got, ok := retained.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("deadline = %v/%v, want %v", got, ok, deadline)
	}
	// This is the asynchronous lease cancellation that previously queried
	// the recycled fasthttp parent's mutable user-value map.
	_, release := context.WithCancel(retained)
	release()
	cancel()
	select {
	case <-retained.Done():
		if retained.Err() != context.Canceled {
			t.Fatalf("cancellation error = %v", retained.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("middleware cancellation was lost")
	}
}

func TestRepositoryHTTPContextUsesStableDefault(t *testing.T) {
	var request fasthttp.RequestCtx
	request.Request.SetRequestURI("http://proxy.example/ubuntu/pool/package.deb")
	app := fiber.New()
	c := app.AcquireCtx(&request)
	var retained context.Context
	handler := repositoryHTTPHandler(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retained = r.Context()
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := handler(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	request.Request.Reset()
	if retained != context.Background() {
		t.Fatalf("default context retains request state: %T", retained)
	}
}

func TestRepositoryHTTPRequestMetadataSurvivesRequestReuse(t *testing.T) {
	var request fasthttp.RequestCtx
	originalURI := "http://alice.example/ubuntu/pool/package.deb?tenant=alice"
	request.Request.SetRequestURI(originalURI)
	request.Request.Header.SetMethod(http.MethodGet)
	request.Request.Header.SetProtocol("HTTP/1.1")
	request.Request.Header.Set("Accept-Language", "en")
	request.Request.Header.Set("X-Tenant", "alice")
	app := fiber.New()
	c := app.AcquireCtx(&request)
	var retained *http.Request
	handler := repositoryHTTPHandler(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retained = r
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := handler(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	request.Request.Reset()
	request.Request.SetRequestURI("http://bruce.example/ubuntu/pool/package.deb?tenant=bruce")
	request.Request.Header.SetMethod(http.MethodPut)
	request.Request.Header.SetProtocol("HTTP/1.0")
	request.Request.Header.Set("Accept-Language", "fr")
	request.Request.Header.Set("X-Tenant", "bruce")
	// Fiber's adaptor hands net/http the routed origin-form target. Preserve
	// that target and its authority rather than the absolute-form request line.
	if retained.Method != http.MethodGet || retained.Proto != "HTTP/1.1" || retained.Host != "alice.example" || retained.RequestURI != "/ubuntu/pool/package.deb?tenant=alice" || retained.URL.RawQuery != "tenant=alice" || retained.Header.Get("Accept-Language") != "en" || retained.Header.Get("X-Tenant") != "alice" {
		t.Fatalf("recycled request changed retained selection metadata: method=%s proto=%s host=%s target=%s URL=%s headers=%v", retained.Method, retained.Proto, retained.Host, retained.RequestURI, retained.URL, retained.Header)
	}
}

func TestRepositoryAsyncVarySelectionSurvivesRequestReuse(t *testing.T) {
	lifecycle, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	oldClock := httpcache.Clock
	entered, release := make(chan struct{}, 1), make(chan struct{})
	httpcache.Clock = func() time.Time {
		var pcs [12]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, "(*Handler).storeResource.func1") {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
				break
			}
			if !more {
				break
			}
		}
		return time.Now()
	}
	var releaseOnce sync.Once
	releaseStore := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() { releaseStore(); httpcache.Writes.Wait(); httpcache.Clock = oldClock }()
	cache := httpcache.NewMemoryCacheWithConfig(httpcache.DefaultCacheConfig().WithCleanupInterval(0))
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	var calls int
	handler := httpcache.NewHandler(cache, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body := "language=" + r.Header.Get("Accept-Language")
		w.Header().Set("Date", time.Now().Format(http.TimeFormat))
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Vary", "Accept-Language")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	}))
	var request fasthttp.RequestCtx
	request.Request.SetRequestURI("http://proxy.example/ubuntu/pool/a.deb")
	request.Request.Header.SetMethod("GET")
	request.Request.Header.Set("Accept-Language", "en")
	app := fiber.New()
	bridge := repositoryHTTPHandler(lifecycle, handler)
	c := app.AcquireCtx(&request)
	if err := bridge(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		releaseStore()
		t.Fatal("async Store did not begin")
	}
	request.Request.Reset()
	request.Request.SetRequestURI("http://proxy.example/ubuntu/pool/a.deb")
	request.Request.Header.SetMethod("GET")
	request.Request.Header.Set("Accept-Language", "fr")
	releaseStore()
	httpcache.Writes.Wait()
	var warm fasthttp.RequestCtx
	warm.Request.SetRequestURI("http://proxy.example/ubuntu/pool/a.deb")
	warm.Request.Header.SetMethod("GET")
	warm.Request.Header.Set("Accept-Language", "en")
	c = app.AcquireCtx(&warm)
	if err := bridge(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	if label, body := string(warm.Response.Header.Peek("X-Cache")), string(warm.Response.Body()); label != "HIT" || body != "language=en" || calls != 1 {
		t.Fatalf("handler return canceled asynchronous publication: label=%s body=%s calls=%d", label, body, calls)
	}
	c = app.AcquireCtx(&request)
	if err := bridge(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	httpcache.Writes.Wait()
	if got := string(request.Response.Body()); got != "language=fr" {
		t.Fatalf("async variant indexed recycled metadata: got=%q label=%q calls=%d, want language=fr", got, request.Response.Header.Peek("X-Cache"), calls)
	}
}
