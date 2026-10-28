package sharedcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type edgeRoundTripper func(*http.Request) (*http.Response, error)

func (f edgeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingStageBudget struct {
	lease httpcache.StagingLease
	err   error
}

func (b failingStageBudget) NewLease(context.Context, httpcache.StageKind) (httpcache.StagingLease, error) {
	return b.lease, b.err
}

func TestVerifiedTransportAdmissionFailuresCloseOwnedResources(t *testing.T) {
	body := "index"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	p, err := New(&Config{Repositories: []Repository{{ID: "repo", Bases: []string{"http://example/archive/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://example/archive/dists/trixie/main/binary-amd64/by-hash/SHA256/"+digest, nil)
	sentinel := errors.New("upstream failure")
	res, err := p.WrapTransport(edgeRoundTripper(func(*http.Request) (*http.Response, error) { return nil, sentinel }), t.TempDir()).RoundTrip(req)
	if res != nil {
		defer res.Body.Close()
	}
	if res != nil || !errors.Is(err, sentinel) {
		t.Fatal("lost upstream error")
	}
	budget := &testBudget{limit: 100}
	lease := &testLease{b: budget}
	for _, b := range []httpcache.StagingBudget{failingStageBudget{}, failingStageBudget{lease: lease, err: sentinel}} {
		closer := &trackedBody{Reader: strings.NewReader(body)}
		tr := p.WrapTransportWithBudget(edgeRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: closer, ContentLength: 5, Header: make(http.Header)}, nil
		}), t.TempDir(), b)
		res, err := tr.RoundTrip(req)
		if res != nil {
			defer res.Body.Close()
		}
		if res != nil || !errors.Is(err, httpcache.ErrStagingBudgetExceeded) {
			t.Fatalf("failed admission: %v", err)
		}
		if !closer.closed {
			t.Fatal("upstream body leaked")
		}
	}
	if !lease.closed {
		t.Fatal("lease returned with error leaked")
	}
	closer := &trackedBody{Reader: strings.NewReader(body)}
	tr := p.WrapTransportWithBudget(edgeRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: closer, ContentLength: 5, Header: make(http.Header)}, nil
	}), "/nonexistent/apt-byhash-test", budget)
	res, err = tr.RoundTrip(req)
	if res != nil {
		defer res.Body.Close()
	}
	if res != nil || err == nil || !closer.closed {
		t.Fatal("temporary file failure not propagated")
	}
	if budget.bytes() != 0 {
		t.Fatal("failed spool retains budget")
	}
	var disabled *Policy
	base := edgeRoundTripper(func(*http.Request) (*http.Response, error) { return nil, sentinel })
	res, err = disabled.WrapTransport(base, "").RoundTrip(req)
	if res != nil {
		defer res.Body.Close()
	}
	if res != nil || !errors.Is(err, sentinel) {
		t.Fatal("disabled wrapper intercepted request")
	}
	if key, err := disabled.RequestKey(req); err != nil || key.String() == "" {
		t.Fatal("disabled policy lost ordinary request key")
	}
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }
func TestChargedWriterReleasesFailedWriteAndHonorsCancellation(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "charged")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	b := &testBudget{limit: 10}
	lease, _ := b.NewLease(context.Background(), StageByHash)
	w := &chargedWriter{ctx: context.Background(), f: file, lease: lease}
	if n, err := w.Write([]byte("bytes")); n != 0 || err == nil || b.bytes() != 0 {
		t.Fatal("failed write retained reservation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.ctx = ctx
	if _, err := w.Write([]byte("bytes")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled writer accepted bytes")
	}
	lease.Close()
}

func TestNonSharedTransportKeepsOriginalRequest(t *testing.T) {
	p, err := New(&Config{Repositories: []Repository{{ID: "repo", Bases: []string{"http://example/archive/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://example/archive/dists/trixie/InRelease", nil)
	called := false
	tr := p.WrapTransport(edgeRoundTripper(func(r *http.Request) (*http.Response, error) {
		called = true
		if r != req {
			t.Fatal("mutable request changed")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}), t.TempDir())
	res, err := tr.RoundTrip(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil || !called {
		t.Fatal("mutable request did not reach ordinary transport")
	}
}
