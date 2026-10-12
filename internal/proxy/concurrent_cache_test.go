package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestRepositoryConcurrentColdDownloads(t *testing.T) {
	var hits atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("complete-package"))
	}))
	defer upstream.Close()
	// Always unblock the upstream even when an assertion fails.
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default()})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := httpcache.NewHandlerWithOptions(cache, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := cache.Close(); err != nil {
			t.Error(err)
		}
	})
	request := func(ctx context.Context) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://proxy.example/ubuntu/pool/concurrent.deb", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)
		return rec
	}
	results := make(chan *httptest.ResponseRecorder, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { results <- request(ctx) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("leader never reached upstream")
	}
	// A canceled follower must leave the miss queue without ending the leader.
	followerBase, cancelFollower := context.WithCancel(ctx)
	followerCtx := &missWaitingContext{Context: followerBase, waiting: make(chan struct{})}
	followerDone := make(chan struct{})
	go func() { request(followerCtx); close(followerDone) }()
	select {
	case <-followerCtx.waiting:
	case <-ctx.Done():
		t.Fatal("follower never joined the miss queue")
	}
	cancelFollower()
	select {
	case <-followerDone:
	case <-ctx.Done():
		t.Fatal("canceled follower blocked")
	}
	// Done is evaluated by the cache's follower select only after claimMiss.
	// Observe that boundary for every client before allowing the leader to finish.
	for i := 0; i < 15; i++ {
		waitingCtx := &missWaitingContext{Context: ctx, waiting: make(chan struct{})}
		go func() { results <- request(waitingCtx) }()
		select {
		case <-waitingCtx.waiting:
		case <-ctx.Done():
			t.Fatal("client never joined the miss queue")
		}
	}
	unblock()
	for i := 0; i < 16; i++ {
		select {
		case rec := <-results:
			if rec.Code != 200 || rec.Body.String() != "complete-package" {
				t.Fatalf("incomplete concurrent response: %d %q", rec.Code, rec.Body.String())
			}
		case <-ctx.Done():
			t.Fatal("concurrent requests blocked")
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("cold download contacted upstream %d times, want 1", hits.Load())
	}
	rec := request(ctx)
	if rec.Body.String() != "complete-package" || hits.Load() != 1 {
		t.Fatal("completed package was not reused")
	}
}

// The cache has no public miss-queue hook. Observe the context Done call made
// directly by Handler.resolveMiss's follower select, excluding earlier context
// propagation and transport cancellation checks. This intentionally pins the
// dependency boundary: moving that select requires updating this test probe.
type missWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *missWaitingContext) Done() <-chan struct{} {
	if pc, _, _, ok := runtime.Caller(1); ok {
		if fn := runtime.FuncForPC(pc); fn != nil && fn.Name() == "github.com/soulteary/httpcache-kit/v4.(*Handler).resolveMiss" {
			ctx.once.Do(func() { close(ctx.waiting) })
		}
	}
	return ctx.Context.Done()
}
