package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestUnknownLengthDownloadTempFileCleanup(t *testing.T) {
	dir := t.TempDir()
	req := httptest.NewRequest("GET", "http://mirror.example/package.deb", nil)
	transport := &completeUnknownBodyTransport{tempDir: dir, RoundTripper: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("complete"))}, nil
	})}
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := res.Body.(*downloadFile)
	if !ok {
		t.Fatal("unknown body was not spooled")
	}
	path := file.path
	if filepath.Dir(path) != dir {
		t.Fatalf("spool used %q, want %q", filepath.Dir(path), dir)
	}
	if res.ContentLength != 8 || res.Header.Get("Content-Length") != "8" {
		t.Fatal("completed length not published")
	}
	body, err := io.ReadAll(res.Body)
	if err != nil || string(body) != "complete" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary download retained: %v", err)
	}
}

func TestAbortSafeHandlerDoesNotHideUnexpectedPanics(t *testing.T) {
	for _, value := range []any{http.ErrAbortHandler, "programming error"} {
		func() {
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				h := &abortSafeHandler{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(value) })}
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			}()
			if value == http.ErrAbortHandler {
				if recovered != nil {
					t.Fatal("intentional abort escaped")
				}
			} else if recovered != value {
				t.Fatal("unexpected panic swallowed")
			}
		}()
	}
}

func TestProxyUsesConfiguredDownloadDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(dir, "unavailable-system-temp"))
	ps, err := NewPackageStruct(Options{State: newTestState(), Registry: newTestRegistry(), DownloadTempDir: dir, Mode: distro.TypeUbuntu, TransportOverride: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("complete")), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, httptest.NewRequest("GET", "http://proxy.example/ubuntu/pool/package.deb", nil))
	if rec.Code != 200 || rec.Body.String() != "complete" {
		t.Fatalf("configured spool unavailable: %d %q", rec.Code, rec.Body.String())
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("spool files retained: %v", files)
	}
}

func TestUnknownLengthMissingSpoolClosesUpstream(t *testing.T) {
	body := &guardTrackingBody{Reader: strings.NewReader("complete")}
	transport := &completeUnknownBodyTransport{tempDir: filepath.Join(t.TempDir(), "missing"), RoundTripper: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}, nil
	})}
	failed, err := transport.RoundTrip(httptest.NewRequest("GET", "http://example/pkg", nil))
	if failed != nil {
		defer failed.Body.Close()
	}
	if failed != nil || err == nil || !body.closed {
		t.Fatal("failed spool leaked upstream")
	}
	transport = &completeUnknownBodyTransport{tempDir: t.TempDir(), RoundTripper: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader("complete"))}, nil
	})}
	res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://example/pkg", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Length") != "8" {
		t.Fatal("nil header was not initialized after spool")
	}
}

type guardTrackingBody struct {
	io.Reader
	closed bool
}

func (b *guardTrackingBody) Close() error { b.closed = true; return nil }
