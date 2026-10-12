package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func budgetTestTransport(dir string, perResponse, total int64) *completeUnknownBodyTransport {
	return &completeUnknownBodyTransport{tempDir: dir, maxBytes: perResponse, budget: &spoolBudget{limit: total}, RoundTripper: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("12345678"))}, nil
	})}
}
func TestSpoolPerResponseLimitAndCleanup(t *testing.T) {
	dir := t.TempDir()
	transport := budgetTestTransport(dir, 7, 16)
	req := httptest.NewRequest("GET", "http://mirror.example/package.deb", nil)
	req.Header.Set("Upgrade", "ignored-upgrade")
	if res, err := transport.RoundTrip(req); err == nil {
		res.Body.Close()
		t.Fatal("oversized body accepted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 || transport.budget.used != 0 {
		t.Fatalf("failed spool leaked resources: %v %v used=%d", files, err, transport.budget.used)
	}
	transport.maxBytes = 8
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if transport.budget.used != 0 {
		t.Fatal("exact-limit response retained budget")
	}
}
func TestConcurrentSpoolBudgetAndReuse(t *testing.T) {
	dir := t.TempDir()
	transport := budgetTestTransport(dir, 8, 16)
	results := make(chan *http.Response, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.example/package.deb", nil)) //nolint:bodyclose // Ownership transfers to results; the receiver closes every successful response.
			if err == nil {
				results <- res
			}
		}()
	}
	workers.Wait()
	close(results)
	reserved := transport.budget.used
	accepted := 0
	for res := range results {
		accepted++
		res.Body.Close()
	}
	if reserved != 16 || accepted != 2 || transport.budget.used != 0 {
		t.Fatalf("accepted=%d retained=%d", accepted, transport.budget.used)
	}
	res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.example/package.deb", nil))
	if err != nil {
		t.Fatalf("released budget not reusable: %v", err)
	}
	res.Body.Close()
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files retained: %v %v", files, err)
	}
}
func TestUpstreamTotalDeadline(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "known"}[known], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if known {
					w.Header().Set("Content-Length", "8")
				}
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer upstream.Close()
			dir := t.TempDir()
			transport := &completeUnknownBodyTransport{RoundTripper: http.DefaultTransport, tempDir: dir, timeout: 50 * time.Millisecond}
			req, _ := http.NewRequest("GET", upstream.URL, nil)
			done := make(chan error, 1)
			go func() {
				res, err := transport.RoundTrip(req)
				if err == nil {
					_, err = io.ReadAll(res.Body)
					res.Body.Close()
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("stalled body completed without timeout")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("body deadline not enforced")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 || transport.budget.used != 0 {
				t.Fatalf("timeout retained resources: %v %v used=%d", files, err, transport.budget.used)
			}
		})
	}
}

type upgradeFixtureBody struct{ bytes.Buffer }

func (b *upgradeFixtureBody) Close() error { return nil }
func TestDownloadLimitsPreserveUpgradeBody(t *testing.T) {
	var requestCtx context.Context
	transport := &completeUnknownBodyTransport{RoundTripper: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requestCtx = r.Context()
		return &http.Response{StatusCode: 101, Body: &upgradeFixtureBody{}, Header: make(http.Header)}, nil
	})}
	res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		res.Body.Close()
		t.Fatal("upgrade lost writable body")
	}
	if requestCtx.Err() != nil {
		body.Close()
		t.Fatal("upgrade context canceled before use")
	}
	if _, err := body.Write([]byte("ok")); err != nil {
		body.Close()
		t.Fatal(err)
	}
	body.Close()
	if requestCtx.Err() == nil {
		t.Fatal("upgrade close retained timeout")
	}
}
