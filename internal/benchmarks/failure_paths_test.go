package benchmarks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

type failedProbeTransport func(*http.Request) (*http.Response, error)

func (f failedProbeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failedProbeBody struct{}

func (failedProbeBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedProbeBody) Close() error             { return nil }

func TestBenchmarkRejectsCancelledInvalidAndBrokenFallback(t *testing.T) {
	e := NewEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Benchmark(ctx, "http://example.invalid/", "", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled benchmark: %v", err)
	}
	if _, err := singleBenchmark(context.Background(), e.client, ":bad-url"); err == nil {
		t.Fatal("invalid request URL accepted")
	}
	for _, readFailure := range []bool{false, true} {
		client := &http.Client{Transport: failedProbeTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodHead {
				return &http.Response{StatusCode: 405, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			}
			if !readFailure {
				return nil, io.ErrClosedPipe
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: failedProbeBody{}, Request: r}, nil
		})}
		if _, err := singleBenchmark(context.Background(), client, "http://example.invalid/"); err == nil {
			t.Fatal("broken fallback accepted")
		}
	}
}

func TestFailedMirrorElectionDoesNotPopulateCache(t *testing.T) {
	e := NewEngine()
	if _, err := e.GetTheFastestMirrorWithCache(77, []string{":invalid"}, ""); err == nil {
		t.Fatal("invalid mirror elected")
	}
	if _, ok := e.Cache().GetCachedResult(77); ok {
		t.Fatal("failure cached as healthy mirror")
	}
	// Static Alpine candidate discovery must work without upstream policy/geo IO.
	if got := e.MirrorCandidates(distro.NewBuiltinRegistry(), distro.TypeAlpine); len(got) == 0 {
		t.Fatal("no built-in Alpine candidates")
	}
}
