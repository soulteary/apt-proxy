package s3vfs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

type s3FaultTransport struct {
	base  *memoryS3
	fault func(*http.Request) (int, string, bool)
}

func (f *s3FaultTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.fault != nil {
		if status, body, ok := f.fault(r); ok {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
	}
	return f.base.RoundTrip(r)
}
func TestS3ConfigurationAndHTTPFailures(t *testing.T) {
	base := &memoryS3{objects: map[string]string{}}
	transport := &s3FaultTransport{base: base}
	cfg := Config{Endpoint: "s3.test", Region: "us-east-1", Bucket: "test-bucket", UsePathStyle: true, HTTPClient: &http.Client{Transport: transport}}
	// New explicitly accepts nil and supplies a background context for VFS users.
	fs, err := New(nil, cfg) //nolint:staticcheck // Exercise the supported nil-context normalization contract (SA1012).
	if err != nil {
		t.Fatal(err)
	}
	if fs.inlineMax != inlineSpillThreshold || fs.tmpDir != os.TempDir() {
		t.Fatal("missing defaults")
	}
	bad := cfg
	bad.Endpoint = "http://bad/path"
	if _, err := New(context.Background(), bad); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	for _, code := range []int{403, 404} {
		transport.fault = func(r *http.Request) (int, string, bool) {
			return code, "<Error><Code>AccessDenied</Code></Error>", r.Method == http.MethodHead && r.URL.Path == "/test-bucket/"
		}
		if _, err := New(context.Background(), cfg); err == nil {
			t.Fatalf("startup accepted bucket status %d", code)
		}
		if err := fs.HealthCheck(context.Background()); err == nil {
			t.Fatalf("healthcheck accepted %d", code)
		}
	}
	transport.fault = func(r *http.Request) (int, string, bool) {
		return 403, "<Error><Code>AccessDenied</Code></Error>", true
	}
	if _, err := fs.Stat("denied"); err == nil || os.IsNotExist(err) {
		t.Fatalf("denied stat mapped to missing: %v", err)
	}
	if _, err := fs.ReadDir("denied"); err == nil {
		t.Fatal("failed listing accepted")
	}
	if _, err := fs.OpenFile("missing", os.O_RDONLY, 0600); err == nil {
		t.Fatal("read-only missing open accepted")
	}
	if err := fs.Remove("denied"); err == nil || os.IsNotExist(err) {
		t.Fatalf("denied removal: %v", err)
	}
	transport.fault = func(r *http.Request) (int, string, bool) {
		return 404, "<Error><Code>NoSuchKey</Code></Error>", r.Method == http.MethodDelete
	}
	if err := fs.Remove("missing"); !os.IsNotExist(err) {
		t.Fatalf("missing delete: %v", err)
	}
	transport.fault = func(r *http.Request) (int, string, bool) {
		return 200, "<DeleteResult><Error><Key>cache/pkg</Key><Code>AccessDenied</Code><Message>denied</Message></Error></DeleteResult>", r.Method == http.MethodPost
	}
	base.objects["cache/pkg"] = "data"
	if err := fs.Remove("/"); err == nil {
		t.Fatal("root removal accepted")
	}
	if err := fs.Remove("cache/"); err == nil {
		t.Fatal("bulk removal error discarded")
	}
}

type rejectedBudget struct {
	lease httpcache.StagingLease
	err   error
}

func (b rejectedBudget) NewLease(context.Context, httpcache.StageKind) (httpcache.StagingLease, error) {
	return b.lease, b.err
}

type edgeLease struct {
	closed bool
	err    error
}

func (l *edgeLease) Reserve(int64) error { return l.err }
func (l *edgeLease) Release(int64)       {}
func (l *edgeLease) Close()              { l.closed = true }

func TestS3WriterBudgetFailuresRemainSticky(t *testing.T) {
	sentinel := errors.New("no staging")
	lease := &edgeLease{}
	for _, budget := range []httpcache.StagingBudget{rejectedBudget{lease: lease, err: sentinel}, rejectedBudget{}, rejectedBudget{lease: &edgeLease{err: sentinel}}} {
		w := &s3WFile{budget: budget, inlineMax: 8, tmpDir: t.TempDir()}
		if _, err := w.Write([]byte("data")); err == nil {
			t.Fatal("failed budget accepted")
		}
		if _, err := w.Write([]byte("retry")); err == nil {
			t.Fatal("failed writer recovered without budget")
		}
		if err := w.Close(); err == nil {
			t.Fatal("failed writer uploaded")
		}
	}
	if !lease.closed {
		t.Fatal("lease returned with error leaked")
	}
	// The spill reserve must fail without publishing or retaining a descriptor.
	w := &s3WFile{budget: rejectedBudget{lease: &edgeLease{err: sentinel}}, inlineMax: 1, tmpDir: t.TempDir()}
	if _, err := w.Write([]byte("spill")); err == nil {
		t.Fatal("spill budget failure ignored")
	}
	_ = w.Close()
	w = &s3WFile{inlineMax: 1, tmpDir: filepath.Join(t.TempDir(), "missing")}
	if _, err := w.Write([]byte("spill")); err == nil {
		t.Fatal("missing spool directory accepted")
	}
	_ = w.Close()
	w = &s3WFile{written: 3}
	if off, err := w.Seek(3, io.SeekStart); err != nil || off != 3 {
		t.Fatal("no-op absolute seek rejected")
	}
	var buffer inlineBuffer
	if n, err := buffer.Write(nil); n != 0 || err != nil || buffer.Len() != 0 {
		t.Fatal("empty write allocated bytes")
	}
}
