package mirrorhealth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeFailoverCountsIncompleteSuccessfulBodies(t *testing.T) {
	for _, kind := range []string{"fixed-length", "chunked-disconnect", "read-timeout"} {
		t.Run(kind, func(t *testing.T) {
			secondary := newFixture(t)
			var primaryPackages atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/InRelease") {
					_, _ = io.WriteString(w, "same signed snapshot")
					return
				}
				primaryPackages.Add(1)
				if kind != "chunked-disconnect" {
					w.Header().Set("Content-Length", "64")
				}
				_, _ = io.WriteString(w, "partial")
				w.(http.Flusher).Flush()
				switch kind {
				case "chunked-disconnect":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
				case "read-timeout":
					<-r.Context().Done()
				}
			}))
			defer primary.Close()
			base := &http.Transport{}
			defer base.CloseIdleConnections()
			c, err := New(&Config{Enabled: true, FailureThreshold: 2, ProbeTimeoutSeconds: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.URL + "/ubuntu/", secondary.server.URL + "/alternate/"}}}}, base, base, nil)
			if err != nil {
				t.Fatal(err)
			}
			fixture := &fixture{server: primary}
			learn(t, c, fixture, "jammy")
			for attempt := 1; attempt <= 2; attempt++ {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				req, _ := http.NewRequestWithContext(ctx, "GET", primary.URL+"/ubuntu/pool/incomplete.deb", nil)
				res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
				if err != nil {
					cancel()
					t.Fatal(err)
				}
				if failures := c.State().Repositories[0].Failures[0]; failures != attempt-1 {
					cancel()
					closeResponse(res)
					t.Fatalf("successful headers erased failures: got=%d want=%d", failures, attempt-1)
				}
				_, readErr := io.ReadAll(res.Body)
				_ = res.Body.Close()
				cancel()
				if readErr == nil || c.State().Repositories[0].Failures[0] != attempt || secondary.requests.Load() != 0 {
					t.Fatalf("incomplete body ignored: err=%v health=%+v candidate=%d", readErr, c.State(), secondary.requests.Load())
				}
			}
			if status, body := fetch(t, c, fixture, "pool/incomplete.deb", nil); status != 200 || body != "complete package" || primaryPackages.Load() != 2 {
				t.Fatalf("body failures did not trigger verified next-request failover: status=%d body=%q primary=%d", status, body, primaryPackages.Load())
			}
		})
	}
}

func TestRuntimeFailoverCompleteEOFAndCallerCancellation(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	learn(t, c, primary, "jammy")
	repo := c.repos["ubuntu"]
	repo.mu.Lock()
	repo.failures[0] = 1
	repo.mu.Unlock()
	req, _ := http.NewRequest("GET", primary.server.URL+"/ubuntu/pool/complete.deb", nil)
	res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
	if err != nil {
		t.Fatal(err)
	}
	if c.State().Repositories[0].Failures[0] != 1 {
		t.Fatal("headers reset prior health failures")
	}
	_, err = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || c.State().Repositories[0].Failures[0] != 0 {
		t.Fatal("complete body did not establish recovery")
	}
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	res, err = c.RoundTrip(req, "ubuntu", c.Epoch())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	_, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if c.State().Repositories[0].Failures[0] != 0 {
		t.Fatal("client cancellation counted as a mirror failure")
	}
}

func TestHealthBodyRejectsEOFWithLengthMismatch(t *testing.T) {
	for _, expected := range []int64{2, 8} {
		var terminalCalls int
		body := &healthBody{ReadCloser: io.NopCloser(strings.NewReader("short")), ctx: context.Background(), expected: expected, finish: func(complete bool) {
			terminalCalls++
			if complete {
				t.Fatal("mismatched length counted as complete")
			}
		}}
		_, err := io.ReadAll(body)
		_, _ = body.Read(make([]byte, 1))
		_ = body.Close()
		if err != io.ErrUnexpectedEOF || terminalCalls != 1 {
			t.Fatalf("length mismatch: expected=%d err=%v terminal=%d", expected, err, terminalCalls)
		}
	}
}

func TestRuntimeFailoverLateBodyCannotChangeReplacementHealth(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "new-epoch"}[refresh], func(t *testing.T) {
			primary, secondary := newFixture(t), newFixture(t)
			c := newController(t, primary, secondary)
			learn(t, c, primary, "jammy")
			req, _ := http.NewRequest("GET", primary.server.URL+"/ubuntu/pool/late.deb", nil)
			late, err := c.RoundTrip(req, "ubuntu", c.Epoch())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = late.Body.Close() }()
			primary.fail.Store(true)
			if status, _ := fetch(t, c, primary, "pool/switch.deb", nil); status != 200 {
				t.Fatal("replacement was not selected")
			}
			if refresh {
				c.Reset()
			}
			repo := c.repos["ubuntu"]
			repo.mu.Lock()
			repo.failures[0], repo.failures[1] = 3, 2
			repo.mu.Unlock()
			if _, err := io.ReadAll(late.Body); err != nil {
				t.Fatal(err)
			}
			state := c.State().Repositories[0]
			if state.Failures[0] != 3 || state.Failures[1] != 2 {
				t.Fatalf("late body changed current health: %+v", state)
			}
		})
	}
}

func TestRuntimeFailoverBodyFailureStillRequiresKnownSnapshot(t *testing.T) {
	secondary := newFixture(t)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "64")
		_, _ = io.WriteString(w, "partial")
	}))
	defer primary.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.URL + "/ubuntu/", secondary.server.URL + "/alternate/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		req, _ := http.NewRequest("GET", primary.URL+"/ubuntu/pool/no-snapshot.deb", nil)
		res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
		if err != nil || res == nil {
			t.Fatalf("unknown snapshot stopped configured route: response=%v err=%v", res, err)
		}
		_, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr == nil || c.State().Repositories[0].Failures[0] != attempt || secondary.requests.Load() != 0 {
			t.Fatalf("unknown snapshot permitted body-error switch: err=%v state=%+v candidate=%d", readErr, c.State(), secondary.requests.Load())
		}
	}
}
