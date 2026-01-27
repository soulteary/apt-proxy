package proxy

import (
	"fmt"
	"net/http"
	"time"

	httpkit "github.com/soulteary/http-kit"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	tracing "github.com/soulteary/tracing-kit"
)

// RetryableTransport wraps an http.RoundTripper with retry logic and tracing support
type RetryableTransport struct {
	baseTransport http.RoundTripper
	retryOpts     *httpkit.RetryOptions
}

// NewRetryableTransport creates a new RetryableTransport with http-kit integration
func NewRetryableTransport(baseTransport http.RoundTripper) *RetryableTransport {
	// Configure retry options using http-kit defaults
	retryOpts := httpkit.DefaultRetryOptions()
	retryOpts.MaxRetries = 3
	retryOpts.RetryDelay = 100 * time.Millisecond
	retryOpts.MaxRetryDelay = 2 * time.Second
	retryOpts.BackoffMultiplier = 2.0

	return &RetryableTransport{
		baseTransport: baseTransport,
		retryOpts:     retryOpts,
	}
}

// RoundTrip implements http.RoundTripper interface with retry and tracing support
func (rt *RetryableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// Start tracing span
	spanCtx, span := tracing.StartSpan(ctx, "proxy.upstream.request")
	defer span.End()

	// Set span attributes
	tracing.SetSpanAttributesFromMap(span, map[string]interface{}{
		"http.method":     req.Method,
		"http.url":        req.URL.String(),
		"http.scheme":     req.URL.Scheme,
		"http.host":       req.URL.Host,
		"http.target":     req.URL.Path,
		"http.user_agent": req.UserAgent(),
	})

	// Inject trace context into request headers
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(spanCtx, propagation.HeaderCarrier(req.Header))

	// Perform request with retry logic
	var lastErr error
	maxAttempts := rt.retryOpts.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Calculate delay before retry
			delay := rt.retryOpts.CalculateRetryDelay(attempt - 1)

			// Wait before retry
			select {
			case <-ctx.Done():
				tracing.RecordError(span, ctx.Err())
				return nil, ctx.Err()
			case <-time.After(delay):
			}

			// Log retry attempt
			tracing.SetSpanAttributes(span, map[string]string{
				"retry.attempt": fmt.Sprintf("%d", attempt),
			})
		}

		// Make the request
		resp, err := rt.baseTransport.RoundTrip(req)
		if err != nil {
			lastErr = err
			if !rt.retryOpts.IsRetryableError(err, 0) {
				tracing.RecordError(span, err)
				return nil, fmt.Errorf("failed to execute request: %w", err)
			}
			if attempt >= rt.retryOpts.MaxRetries {
				tracing.RecordError(span, fmt.Errorf("failed after %d retries: %w", rt.retryOpts.MaxRetries, lastErr))
				return nil, fmt.Errorf("failed to execute request after retries: %w", lastErr)
			}
			continue
		}

		// Check if status code is retryable and we have retries left
		if rt.retryOpts.IsRetryableError(nil, resp.StatusCode) && attempt < rt.retryOpts.MaxRetries {
			// Close response body before retry
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("server error: status %d", resp.StatusCode)
			continue
		}

		// Success or non-retryable error - record response attributes
		tracing.SetSpanAttributes(span, map[string]string{
			"http.status_code": fmt.Sprintf("%d", resp.StatusCode),
			"http.status_text": resp.Status,
		})

		if resp.StatusCode >= 400 {
			tracing.SetSpanStatus(span, codes.Error, resp.Status)
		} else {
			tracing.SetSpanStatus(span, codes.Ok, "")
		}

		return resp, nil
	}

	// This should not be reached, but handle it anyway
	if lastErr != nil {
		tracing.RecordError(span, lastErr)
		return nil, fmt.Errorf("failed after retries: %w", lastErr)
	}
	return nil, fmt.Errorf("no attempts made")
}

// SetRetryOptions allows customizing retry behavior
func (rt *RetryableTransport) SetRetryOptions(opts *httpkit.RetryOptions) {
	rt.retryOpts = opts
}
