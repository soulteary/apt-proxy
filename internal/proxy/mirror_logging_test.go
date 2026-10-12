package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
	logger "github.com/soulteary/logger-kit/v3"
	"github.com/soulteary/tracing-kit/v2/tracingtest"
)

type privateLogTransport func(*http.Request) (*http.Response, error)

func (f privateLogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMirrorLifecycleLogsRedactCredentials(t *testing.T) {
	var output bytes.Buffer
	previous := logger.Default()
	logger.SetDefault(logger.New(logger.Config{Level: logger.InfoLevel, Output: &output, Format: logger.FormatJSON}))
	t.Cleanup(func() { logger.SetDefault(previous) })
	raw := "https://alice:password-secret@mirror.example/ubuntu/?custom_auth=query-secret"
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, raw)
	reg := newTestRegistry()
	engine := benchmarks.NewEngine()
	for _, step := range []struct {
		name  string
		build func() *URLRewriter
	}{
		{"synchronous startup", func() *URLRewriter { return createRewriter(distro.TypeUbuntu, st, reg, engine) }},
		{"asynchronous startup", func() *URLRewriter {
			return createRewriterAsync(distro.TypeUbuntu, st, reg, &URLRewriters{}, engine)
		}},
		{"refresh", func() *URLRewriter { return buildRewriters(distro.TypeUbuntu, st, reg, engine).get(distro.TypeUbuntu) }},
	} {
		t.Run(step.name, func(t *testing.T) {
			output.Reset()
			rw := step.build()
			if rw == nil || rw.mirror == nil || rw.mirror.String() != raw {
				t.Fatal("redaction changed the configured routing URL")
			}
			logs := output.String()
			if strings.Contains(logs, "password-secret") || strings.Contains(logs, "query-secret") {
				t.Fatalf("mirror credentials leaked in Info logs: %s", logs)
			}
			if !strings.Contains(logs, "mirror.example/ubuntu/") || !strings.Contains(logs, "custom_auth") {
				t.Fatalf("mirror diagnostics missing: %s", logs)
			}
		})
	}
}

func TestRepositoryLogsAndSpansRedactCredentials(t *testing.T) {
	_, exporter := tracingtest.Setup(t)
	var output bytes.Buffer
	previous := logger.Default()
	log := logger.New(logger.Config{Level: logger.DebugLevel, Output: &output, Format: logger.FormatJSON})
	logger.SetDefault(log)
	t.Cleanup(func() { logger.SetDefault(previous) })
	ps, err := NewPackageStruct(Options{
		State: newTestState(), Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: log,
		TransportOverride: privateLogTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("custom_auth") != "query-secret" {
				return nil, errors.New("redaction changed request authentication")
			}
			user, password, ok := r.BasicAuth()
			if !ok || user != "alice" || password != "password-secret" {
				return nil, errors.New("redaction changed Basic authentication")
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://alice:password-secret@archive.ubuntu.com/ubuntu/pool/package.deb?custom_auth=query-secret", nil)
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request failed: %d %s", rec.Code, rec.Body.String())
	}
	// The retry wrapper's error spans must redact URL-bearing errors as well as
	// URL attributes, while the returned error keeps its original classification.
	raw := "https://alice:password-secret@mirror.example/ubuntu/?custom_auth=query-secret"
	upstreamErr := &url.Error{Op: "Get", URL: raw, Err: errors.New("connection refused")}
	transport := NewRetryableTransport(privateLogTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != raw {
			t.Error("redaction changed the upstream URL")
		}
		return nil, upstreamErr
	}))
	transport.retryOpts.MaxRetries = 0
	upstreamReq, _ := http.NewRequest("GET", raw, nil)
	response, err := transport.RoundTrip(upstreamReq)
	if response != nil {
		defer response.Body.Close()
	}
	if !errors.Is(err, upstreamErr) {
		t.Fatalf("caller error changed: %v", err)
	}
	logs := output.String()
	if !strings.Contains(logs, "rewrote request URL") {
		t.Fatalf("rewrite log missing: %s", logs)
	}
	spans := exporter.GetSpans()
	if len(spans) < 2 {
		t.Fatalf("request/upstream spans missing: %d", len(spans))
	}
	diagnostics := logs + fmt.Sprint(spans)
	for _, secret := range []string{"password-secret", "query-secret"} {
		if strings.Contains(diagnostics, secret) {
			t.Fatalf("repository diagnostics leaked %q: %s", secret, diagnostics)
		}
	}
	if !strings.Contains(diagnostics, "custom_auth") || !strings.Contains(diagnostics, "connection refused") {
		t.Fatal("redaction lost useful query keys or error diagnostics")
	}
}
