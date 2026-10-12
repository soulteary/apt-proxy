package mirrorhealth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeFailoverReselectionClearsOnlyCompleteResponses(t *testing.T) {
	for _, kind := range []string{"head", "204", "304", "empty-200", "body-200"} {
		t.Run(kind, func(t *testing.T) {
			secondary := newFixture(t)
			var recovering atomic.Bool
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/InRelease") {
					_, _ = io.WriteString(w, "same signed snapshot")
					return
				}
				if !recovering.Load() {
					w.Header().Set("Content-Length", "64")
					_, _ = io.WriteString(w, "partial")
					return
				}
				switch kind {
				case "204", "304":
					status, _ := strconv.Atoi(kind)
					w.WriteHeader(status)
				case "empty-200":
					w.Header().Set("Content-Length", "0")
					w.WriteHeader(200)
				default:
					w.Header().Set("Content-Length", "16")
					if r.Method != http.MethodHead {
						_, _ = io.WriteString(w, "complete package")
					}
				}
			}))
			defer primary.Close()
			base := &http.Transport{}
			defer base.CloseIdleConnections()
			c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.URL + "/ubuntu/", secondary.server.URL + "/alternate/"}}}}, base, base, nil)
			if err != nil {
				t.Fatal(err)
			}
			fixture := &fixture{server: primary}
			learn(t, c, fixture, "jammy")
			req, _ := http.NewRequest(http.MethodGet, primary.URL+"/ubuntu/pool/recovery.deb", nil)
			res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if readErr == nil || c.State().Repositories[0].Failures[0] != 1 {
				t.Fatal("initial truncation did not establish the primary's body failure")
			}
			if status, body := fetch(t, c, fixture, "pool/recovery.deb", nil); status != 200 || body != "complete package" {
				t.Fatal("secondary selection failed")
			}
			recovering.Store(true)
			secondary.fail.Store(true)
			repo := c.repos["ubuntu"]
			repo.mu.Lock()
			// Simulate an elapsed cooldown without slowing the transport fixture.
			repo.nextProbe = time.Time{}
			repo.mu.Unlock()
			if kind == "head" {
				req.Method = http.MethodHead
			}
			res, err = c.RoundTrip(req, "ubuntu", c.Epoch())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			repo.mu.Lock()
			active, failures, bodyFailure := repo.active, repo.failures[0], repo.bodyFailures[0]
			repo.mu.Unlock()
			if active != 0 {
				t.Fatal("recovered primary was not reselected")
			}
			if kind == "body-200" {
				if failures != 1 || !bodyFailure {
					t.Fatalf("body-bearing headers prematurely cleared health: failures=%d bodyFailure=%v", failures, bodyFailure)
				}
				if _, err := io.ReadAll(res.Body); err != nil {
					t.Fatal(err)
				}
			} else if failures != 0 || bodyFailure {
				t.Fatalf("complete empty replay retained prior failure: failures=%d bodyFailure=%v", failures, bodyFailure)
			}
			_ = res.Body.Close()
			if c.State().Repositories[0].Failures[0] != 0 {
				t.Fatal("completed recovery did not clear health")
			}
			// A later request should use the recovered mirror directly, rather
			// than reentering failover because of its historical body failure.
			before := secondary.requests.Load()
			repo.mu.Lock()
			repo.nextProbe = time.Time{}
			repo.mu.Unlock()
			res, err = c.RoundTrip(req, "ubuntu", c.Epoch())
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if secondary.requests.Load() != before {
				t.Fatal("recovered mirror immediately reentered failover")
			}
		})
	}
}
