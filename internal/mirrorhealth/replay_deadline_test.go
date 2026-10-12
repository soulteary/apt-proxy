package mirrorhealth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRuntimeFailoverReplayHeadersShareProbeDeadline(t *testing.T) {
	primary := newFixture(t)
	entered, canceled := make(chan struct{}), make(chan struct{})
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			_, _ = io.WriteString(w, "same signed snapshot")
			return
		}
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	defer secondary.Close()
	base := &http.Transport{ResponseHeaderTimeout: 45 * time.Second}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, ProbeTimeoutSeconds: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.URL + "/archive/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", primary.server.URL+"/ubuntu/pool/stalled.deb", nil)
	started := time.Now()
	res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
	if err != nil {
		closeResponse(res)
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != 503 || string(body) != "primary unavailable" || time.Since(started) > 3*time.Second {
		t.Fatalf("candidate headers escaped deadline: status=%d body=%q err=%v duration=%s", res.StatusCode, body, err, time.Since(started))
	}
	select {
	case <-entered:
	default:
		t.Fatal("candidate replay never started")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("header deadline did not cancel candidate request")
	}
	if len(c.repos["ubuntu"].selecting) != 0 {
		t.Fatal("timed-out candidate retained selection ownership")
	}
}

func TestRuntimeFailoverReplayBodyOutlivesProbeWindow(t *testing.T) {
	primary := newFixture(t)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			_, _ = io.WriteString(w, "same signed snapshot")
			return
		}
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "complete package")
	}))
	defer func() {
		unblock()
		secondary.Close()
	}()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, ProbeTimeoutSeconds: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.URL + "/archive/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", primary.server.URL+"/ubuntu/pool/slow-body.deb", nil)
	res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
	if err != nil {
		closeResponse(res)
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	// The artifact is allowed to stream longer than the selection budget once
	// its headers have arrived; selection's deferred cancel must not close it.
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("original download context expired before body test")
	}
	unblock()
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || string(body) != "complete package" || res.Request != req || req.Context().Err() != nil {
		t.Fatalf("selection canceled returned artifact: body=%q err=%v request-context=%v", body, err, req.Context().Err())
	}
}
