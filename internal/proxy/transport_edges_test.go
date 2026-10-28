package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpkit "github.com/soulteary/http-kit/v2"
)

type retryNetworkError struct{}

func (retryNetworkError) Error() string   { return "connection reset by peer" }
func (retryNetworkError) Timeout() bool   { return true }
func (retryNetworkError) Temporary() bool { return true }

func TestRetryTransportRequestBodyReplayAndFailure(t *testing.T) {
	for _, mode := range []string{"success", "replay-error", "unreplayable", "exhausted", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest("POST", "http://example/upload", strings.NewReader("payload")).WithContext(ctx)
			req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("payload")), nil }
			sentinel := errors.New("cannot rewind")
			if mode == "replay-error" {
				req.GetBody = func() (io.ReadCloser, error) { return nil, sentinel }
			}
			if mode == "unreplayable" {
				req.GetBody = nil
			}
			calls := 0
			rt := NewRetryableTransport(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "cancelled" {
					cancel()
				}
				body, err := io.ReadAll(r.Body)
				r.Body.Close()
				if err != nil || string(body) != "payload" {
					t.Fatalf("attempt %d body=%q err=%v", calls, body, err)
				}
				if mode == "success" && calls == 2 {
					return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
				}
				return nil, retryNetworkError{}
			}))
			rt.SetRetryOptions(&httpkit.RetryOptions{MaxRetries: 1, RetryDelay: time.Millisecond, MaxRetryDelay: time.Millisecond, BackoffMultiplier: 1})
			res, err := rt.RoundTrip(req)
			if res != nil {
				defer res.Body.Close()
			}
			switch mode {
			case "success":
				if err != nil || res.StatusCode != 200 || calls != 2 {
					t.Fatalf("replay result %v %v calls=%d", res, err, calls)
				}
			case "replay-error":
				if !errors.Is(err, sentinel) || calls != 1 {
					t.Fatalf("replay error %v calls=%d", err, calls)
				}
			case "unreplayable":
				if err == nil || calls != 1 {
					t.Fatalf("unsafe retry calls=%d err=%v", calls, err)
				}
			case "exhausted":
				if err == nil || calls != 2 {
					t.Fatalf("retry exhaustion calls=%d err=%v", calls, err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("cancellation calls=%d err=%v", calls, err)
				}
			}
		})
	}
}
