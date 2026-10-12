package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/apt-proxy/internal/resource"
)

func guardedResourceManager(t *testing.T, objectMax, total int64, active int) *resource.Manager {
	t.Helper()
	m, err := resource.New(resource.Config{MaxObjectBytes: objectMax, TotalTempBytes: total, MaxActiveDownloads: active})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type guardedBody struct {
	io.Reader
	closed atomic.Int32
}

func (b *guardedBody) Close() error { b.closed.Add(1); return nil }
func TestKnownLengthDownloadAdmissionCapsAndRelease(t *testing.T) {
	m := guardedResourceManager(t, 8, 32, 1)
	var calls atomic.Int32
	transport := &completeUnknownBodyTransport{resources: m, RoundTripper: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, ContentLength: 8, Header: make(http.Header), Body: &guardedBody{Reader: strings.NewReader("12345678")}}, nil
	})}
	req := httptest.NewRequest("GET", "http://mirror.test/object", nil)
	first, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if m.Stats().ActiveDownloads != 1 {
		t.Fatal("known-length download bypassed active admission")
	}
	if res, err := transport.RoundTrip(req); !errors.Is(err, resource.ErrAdmission) {
		if res != nil {
			_ = res.Body.Close()
		}
		t.Fatalf("concurrent known-length download admitted: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("rejected download reached upstream")
	}
	_ = first.Body.Close()
	_ = first.Body.Close()
	second, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("slot not reusable: %v", err)
	}
	_ = second.Body.Close()
	if m.Stats().ActiveDownloads != 0 {
		t.Fatalf("download slots retained: %+v", m.Stats())
	}
}
func TestKnownLengthObjectLimitRejectsBeforeHeaders(t *testing.T) {
	m := guardedResourceManager(t, 7, 32, 1)
	body := &guardedBody{Reader: strings.NewReader("12345678")}
	transport := &completeUnknownBodyTransport{resources: m, RoundTripper: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 8, Header: make(http.Header), Body: body}, nil
	})}
	res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.test/object", nil))
	if res != nil {
		_ = res.Body.Close()
	}
	if !errors.Is(err, resource.ErrObjectTooLarge) || res != nil || body.closed.Load() != 1 || m.Stats().ActiveDownloads != 0 {
		t.Fatalf("oversized admission: response=%v err=%v closed=%d stats=%+v", res, err, body.closed.Load(), m.Stats())
	}
}
func TestUnknownLengthObjectAndSharedStageLimitCleanup(t *testing.T) {
	for _, objectLimit := range []int64{7, 8} {
		t.Run(map[bool]string{true: "object-cap", false: "shared-copy-cap"}[objectLimit == 7], func(t *testing.T) {
			m := guardedResourceManager(t, objectLimit, 8, 1)
			// Another live handler/cache copy consumes half the unified budget.
			occupied, err := m.NewLease(context.Background(), "another-copy")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()
			if err := occupied.Reserve(4); err != nil {
				t.Fatal(err)
			}
			transport := &completeUnknownBodyTransport{resources: m, tempDir: t.TempDir(), maxBytes: 16, RoundTripper: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("12345678"))}, nil
			})}
			res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.test/object", nil))
			if res != nil {
				_ = res.Body.Close()
			}
			if err == nil || m.Stats().TemporaryBytes != 4 || m.Stats().ActiveDownloads != 0 {
				t.Fatalf("unknown failure retained resources: err=%v stats=%+v", err, m.Stats())
			}
		})
	}
}
func TestKnownLengthStreamCannotExceedObjectLimit(t *testing.T) {
	m := guardedResourceManager(t, 7, 32, 1)
	transport := &completeUnknownBodyTransport{resources: m, RoundTripper: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 6, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("12345678"))}, nil
	})}
	res, err := transport.RoundTrip(httptest.NewRequest("GET", "http://mirror.test/object", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	got, err := io.ReadAll(res.Body)
	if !errors.Is(err, resource.ErrObjectTooLarge) || string(got) != "1234567" {
		t.Fatalf("misreported stream exceeded cap: %q %v", got, err)
	}
}
