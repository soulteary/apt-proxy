package sharedcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type testBudget struct {
	mu          sync.Mutex
	used, limit int64
}
type testLease struct {
	b      *testBudget
	used   int64
	closed bool
}

func (b *testBudget) NewLease(_ context.Context, _ httpcache.StageKind) (httpcache.StagingLease, error) {
	return &testLease{b: b}, nil
}
func (l *testLease) Reserve(n int64) error {
	l.b.mu.Lock()
	defer l.b.mu.Unlock()
	if l.closed || l.b.used+n > l.b.limit {
		return errors.New("full")
	}
	l.used += n
	l.b.used += n
	return nil
}
func (l *testLease) Release(n int64) {
	l.b.mu.Lock()
	defer l.b.mu.Unlock()
	if n > l.used {
		n = l.used
	}
	l.used -= n
	l.b.used -= n
}
func (l *testLease) Close() {
	l.b.mu.Lock()
	defer l.b.mu.Unlock()
	l.b.used -= l.used
	l.used = 0
	l.closed = true
}
func (b *testBudget) bytes() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.used }
func TestVerifiedSpoolRetainsAndReleasesBudget(t *testing.T) {
	body := []byte("index-body")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	p, err := New(&Config{Repositories: []Repository{{ID: "a", Bases: []string{server.URL + "/ubuntu/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{int64(len(body)), int64(len(body) - 1)} {
		b := &testBudget{limit: limit}
		client := &http.Client{Transport: p.WrapTransportWithBudget(http.DefaultTransport, t.TempDir(), b)}
		res, err := client.Get(server.URL + fmt.Sprintf("/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/%x", sha256.Sum256(body)))
		if limit < int64(len(body)) {
			if !errors.Is(err, httpcache.ErrStagingBudgetExceeded) {
				t.Fatalf("refusal %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if b.bytes() != int64(len(body)) {
				t.Fatal("verified replay released reservation too soon")
			}
			got, err := io.ReadAll(res.Body)
			if err != nil || string(got) != string(body) {
				t.Fatal("body changed")
			}
			_ = res.Body.Close()
			_ = res.Body.Close()
		}
		if b.bytes() != 0 {
			t.Fatal("budget retained after error or body close")
		}
	}
}
func TestPublicRequestRejectsBodiesAndUnknownHeaders(t *testing.T) {
	for _, kind := range []string{"unknown-empty", "body", "unknown-length", "transfer", "trailer"} {
		r := httptest.NewRequest("GET", "https://example/x", nil)
		switch kind {
		case "unknown-empty":
			r.Header["X-Tenant-ID"] = []string{""}
		case "body":
			r.ContentLength = 1
		case "unknown-length":
			r.ContentLength = -1
		case "transfer":
			r.TransferEncoding = []string{"chunked"}
		case "trailer":
			r.Trailer = http.Header{"X-Tenant": []string{""}}
		}
		if publicRequest(r) {
			t.Fatalf("%s request classified public", kind)
		}
	}
}
