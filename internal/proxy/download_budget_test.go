package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestUpgradedConnectionClosesAtDownloadDeadline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprint(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		buffer.Flush()
		io.Copy(io.Discard, conn)
	}))
	defer upstream.Close()
	transport := &completeUnknownBodyTransport{RoundTripper: http.DefaultTransport, timeout: 50 * time.Millisecond}
	req, _ := http.NewRequest("GET", upstream.URL, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "test")
	//nolint:bodyclose // The deferred close owns the body while the goroutine verifies deadline cancellation.
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	done := make(chan error, 1)
	go func() { _, err := res.Body.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked upgrade read completed without an error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("upgrade read survived total download deadline")
	}
}

func TestUpgradedConnectionPreservesHalfClose(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprint(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		buffer.Flush()
		request, err := io.ReadAll(conn)
		if err != nil {
			t.Error(err)
			return
		}
		if string(request) != "request" {
			t.Errorf("upgrade request=%q", request)
			return
		}
		io.WriteString(conn, "answer")
	}))
	defer upstream.Close()
	transport := &completeUnknownBodyTransport{RoundTripper: http.DefaultTransport, timeout: time.Second}
	req, _ := http.NewRequest("GET", upstream.URL, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "test")
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, ok := res.Body.(interface {
		io.ReadWriteCloser
		CloseWrite() error
	})
	if !ok {
		t.Fatal("upgrade lost half-close capability")
	}
	if _, err := body.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := body.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(body)
	if err != nil || string(answer) != "answer" {
		t.Fatalf("half-close reply=%q err=%v", answer, err)
	}
}

func TestSpoolDeletionFailureKeepsBudget(t *testing.T) {
	dir := t.TempDir()
	transport := budgetTestTransport(dir, 8, 8)
	req := httptest.NewRequest("GET", "http://mirror.example/package.deb", nil)
	//nolint:bodyclose // The concrete spool is closed below to exercise failed cleanup and retry.
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	spool := res.Body.(*downloadFile)
	backup := spool.path + ".backup"
	if err := os.Rename(spool.path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(spool.path, 0700); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(spool.path, "prevent-removal")
	if err := os.WriteFile(entry, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err == nil {
		t.Fatal("failed deletion reported success")
	}
	if transport.budget.used != 8 {
		t.Fatal("failed deletion released occupied budget")
	}
	if other, err := transport.RoundTrip(req); err == nil {
		other.Body.Close()
		t.Fatal("new spool admitted while failed cleanup occupied budget")
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(spool.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, spool.path); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatalf("cleanup retry failed: %v", err)
	}
	if transport.budget.used != 0 {
		t.Fatal("successful cleanup retry retained budget")
	}
	if err := spool.Close(); err != nil {
		t.Fatalf("repeated close failed: %v", err)
	}
	if transport.budget.used != 0 {
		t.Fatal("repeated close released budget twice")
	}
}
