package benchmarks

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	logger "github.com/soulteary/logger-kit/v3"
)

type mirrorLogTransport func(*http.Request) (*http.Response, error)

func (f mirrorLogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBenchmarkLogsRedactCredentialsWithoutChangingResults(t *testing.T) {
	var output bytes.Buffer
	previous := logger.Default()
	logger.SetDefault(logger.New(logger.Config{Level: logger.DebugLevel, Output: &output, Format: logger.FormatJSON}))
	t.Cleanup(func() { logger.SetDefault(previous) })
	raw := "https://alice:password-secret@mirror.example/ubuntu/?custom_auth=query-secret"
	engine := NewEngine()
	engine.cache.SetCachedResult(1, raw, DefaultCacheTTL)
	got, err := engine.GetTheFastestMirrorWithCache(1, nil, "")
	if err != nil || got != raw {
		t.Fatalf("cached routing result changed: %q %v", got, err)
	}
	wait := func(engine *Engine, id int, mirrors []string) AsyncBenchmarkResult {
		t.Helper()
		done := make(chan AsyncBenchmarkResult, 1)
		engine.GetTheFastestMirrorAsync(id, mirrors, "", func(result AsyncBenchmarkResult) { done <- result })
		select {
		case result := <-done:
			return result
		case <-time.After(5 * time.Second):
			t.Fatal("benchmark callback timed out")
			return AsyncBenchmarkResult{}
		}
	}
	if result := wait(engine, 1, nil); result.Error != nil || result.FastestMirror != raw {
		t.Fatalf("async cached routing result changed: %+v", result)
	}
	engine.client.Transport = mirrorLogTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("custom_auth") != "query-secret" {
			return nil, errors.New("redaction changed request authentication")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
	})
	if result := wait(engine, 2, []string{raw}); result.Error != nil || result.FastestMirror != raw {
		t.Fatalf("async elected routing result changed: %+v", result)
	}
	engine.client.Transport = mirrorLogTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })
	if result := wait(engine, 3, []string{raw}); result.Error == nil || !strings.Contains(result.Error.Error(), "query-secret") {
		t.Fatalf("caller error lost the original request URL: %+v", result)
	}
	logs := output.String()
	if strings.Contains(logs, "password-secret") || strings.Contains(logs, "query-secret") {
		t.Fatalf("benchmark credentials leaked: %s", logs)
	}
	for _, detail := range []string{"using cached benchmark result", "async: using cached benchmark result", "async: benchmark completed", "connection refused", "mirror.example/ubuntu/", "custom_auth"} {
		if !strings.Contains(logs, detail) {
			t.Fatalf("diagnostic %q missing: %s", detail, logs)
		}
	}
}
