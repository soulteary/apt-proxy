package mirrorhealth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestControllerConfigurationBoundaries(t *testing.T) {
	if c, err := New(nil, nil, nil, nil); c != nil || err != nil {
		t.Fatalf("disabled controller: %v %v", c, err)
	}
	cfg := &Config{Enabled: true, Repositories: []Repository{{ID: "repo", Mirrors: []string{"https://a.example/repo/", "https://b.example/repo/"}}}}
	if _, err := New(cfg, nil, nil, nil); err == nil {
		t.Fatal("missing transports accepted")
	}
	if _, err := New(&Config{Enabled: true}, http.DefaultTransport, http.DefaultTransport, nil); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	var disabled *Controller
	if disabled.Epoch() != 0 || disabled.Reset() != 0 || disabled.State().Enabled {
		t.Fatal("disabled controller has active state")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Repositories = nil },
		func(c *Config) { c.Repositories[0].ID = "" },
		func(c *Config) { c.Repositories = append(c.Repositories, c.Repositories[0]) },
		func(c *Config) { c.Repositories[0].Mirrors = c.Repositories[0].Mirrors[:1] },
		func(c *Config) { c.Repositories[0].PublicQueryKeys = make([]string, 17) },
	} {
		c := *cfg
		c.Repositories = append([]Repository(nil), cfg.Repositories...)
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Fatalf("invalid config accepted %+v", c)
		}
	}
}

func TestControllerBypassesUnmappedAndQueriedMetadata(t *testing.T) {
	a, b := newFixture(t), newFixture(t)
	c := newController(t, a, b)
	for _, path := range []string{"/other/file", "/ubuntu/dists/jammy/InRelease?download=1"} {
		req := httptest.NewRequest("GET", a.server.URL+path, nil)
		res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}
	if len(c.repos["ubuntu"].snapshots) != 0 {
		t.Fatal("unmapped or queried metadata learned a snapshot")
	}
	repo := c.repos["ubuntu"]
	for _, raw := range []string{"%zz", "download=1"} {
		req := httptest.NewRequest("GET", a.server.URL+"/ubuntu/a", nil)
		req.URL.RawQuery = raw
		if raw == "download=1" {
			req.ContentLength = -1
		}
		if publicRequest(req, repo) {
			t.Fatal("malformed query or request body replayable")
		}
	}
}

func TestSnapshotLimitFailsClosedAndRejectsUnverifiedPaths(t *testing.T) {
	a, b := newFixture(t), newFixture(t)
	c := newController(t, a, b)
	repo := c.repos["ubuntu"]
	repo.source = 0
	for i := 0; i < 64; i++ {
		repo.observations[fmt.Sprintf("dists/suite%d/Release", i)] = uint64(i + 1)
	}
	req := httptest.NewRequest("GET", a.server.URL+"/ubuntu/dists/jammy/Release", nil)
	res := &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: 4, Body: io.NopCloser(strings.NewReader("data"))}
	c.observe(req, res, repo, 0, "dists/jammy/Release", c.Epoch())
	_, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !repo.limited || repo.reason != "snapshot-limit" || len(repo.snapshots) != 0 {
		t.Fatal("snapshot limit did not fail closed")
	}
	if keys := relevantKeys(repo, "unrecognized/path"); keys != nil {
		t.Fatalf("unrecognized path keys %v", keys)
	}
	repo.limited = false
	repo.snapshots["dists/suite1/Release"] = snapshot{hash: "old", observed: time.Now()}
	repo.verified["dists/suite1/Release"] = "different"
	if c.verifiedFor(repo, "dists/suite1/file") {
		t.Fatal("mismatched snapshot considered verified")
	}
}

func TestCancelledClientDoesNotMarkMirrorUnhealthy(t *testing.T) {
	a, b := newFixture(t), newFixture(t)
	c := newController(t, a, b)
	repo := c.repos["ubuntu"]
	repo.source = 0
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", a.server.URL+"/ubuntu/pool/file", nil).WithContext(ctx)
	res := &http.Response{StatusCode: 200, ContentLength: 5, Body: io.NopCloser(strings.NewReader("x"))}
	c.observeHealth(req, res, repo, 0, c.Epoch())
	cancel()
	_, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if repo.failures[0] != 0 {
		t.Fatal("client cancellation marked mirror failed")
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	res = &http.Response{StatusCode: 200, ContentLength: 5, Body: io.NopCloser(strings.NewReader("x"))}
	c.observeHealth(req.WithContext(ctx), res, repo, 0, c.Epoch())
	_ = res.Body.Close()
	if repo.failures[0] != 1 {
		t.Fatal("deadline close did not mark incomplete response")
	}
}

func TestFailoverCancelledLockAndStaleEpoch(t *testing.T) {
	a, b := newFixture(t), newFixture(t)
	c := newController(t, a, b)
	repo := c.repos["ubuntu"]
	repo.source = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", a.server.URL+"/ubuntu/pool/file", nil).WithContext(ctx)
	repo.selecting <- struct{}{}
	res, err := c.failover(req, repo, 0, 0, "pool/file", c.Epoch(), nil, errors.New("failure"))
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	if res != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled selection %v %v", res, err)
	}
	<-repo.selecting
	failed := &http.Response{StatusCode: 503}
	res, err = c.failover(req.WithContext(context.Background()), repo, 0, 0, "pool/file", c.Epoch()-1, failed, nil)
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	if res != failed || err != nil {
		t.Fatal("stale epoch selected replacement")
	}
}
