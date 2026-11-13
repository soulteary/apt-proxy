// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestImmutableFallbackTransport(t *testing.T) {
	const target = "http://user:secret@mirror.example/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb?token=secret"
	immutable := &distro.Rule{CacheControl: "max-age=100000, immutable"}
	modifiedSince := http.Header{"If-Modified-Since": {immutableTestModified}}
	noneMatch := http.Header{"If-None-Match": {`"v1"`}}
	upstreamErr := &url.Error{Op: "Get", URL: target, Err: errors.New("connection refused")}
	for _, tc := range []struct {
		name     string
		method   string
		rule     *distro.Rule
		header   http.Header
		status   int // 0 makes the wrapped transport return upstreamErr.
		nilBody  bool
		canceled bool
		fallback bool
	}{
		{name: "transport error", rule: immutable, header: modifiedSince, fallback: true},
		{name: "server error", rule: immutable, header: noneMatch, status: http.StatusServiceUnavailable, fallback: true},
		{name: "server error without body", rule: immutable, header: noneMatch, status: http.StatusBadGateway, nilBody: true, fallback: true},
		{name: "head request", method: http.MethodHead, rule: immutable, header: modifiedSince, status: http.StatusInternalServerError, fallback: true},
		{name: "not found", rule: immutable, header: modifiedSince, status: http.StatusNotFound},
		{name: "not modified", rule: immutable, header: modifiedSince, status: http.StatusNotModified},
		{name: "unconditional transport error", rule: immutable},
		{name: "unconditional server error", rule: immutable, status: http.StatusServiceUnavailable},
		{name: "range", rule: immutable, header: http.Header{"If-Modified-Since": {immutableTestModified}, "Range": {"bytes=0-3"}}, status: http.StatusServiceUnavailable},
		{name: "if-range", rule: immutable, header: http.Header{"If-None-Match": {`"v1"`}, "If-Range": {`"v1"`}}},
		{name: "mutable rule", rule: &distro.Rule{CacheControl: "max-age=3600"}, header: modifiedSince},
		{name: "passthrough rule", rule: passthroughRule, header: modifiedSince, status: http.StatusServiceUnavailable},
		{name: "no rule", header: modifiedSince},
		{name: "unsafe method", method: http.MethodPost, rule: immutable, header: modifiedSince, status: http.StatusServiceUnavailable},
		{name: "canceled request", rule: immutable, header: modifiedSince, canceled: true},
		{name: "canceled request with server error", rule: immutable, header: modifiedSince, status: http.StatusServiceUnavailable, canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			body := &guardedBody{Reader: strings.NewReader("unavailable")}
			var upstream *http.Response
			if tc.status != 0 {
				upstream = &http.Response{StatusCode: tc.status, Header: make(http.Header)}
				if !tc.nilBody {
					upstream.Body = body
				}
			}
			transport := &immutableFallbackTransport{
				log: logger.New(logger.Config{Level: logger.InfoLevel, Output: &logs, Format: logger.FormatJSON}),
				RoundTripper: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					if upstream == nil {
						return nil, upstreamErr
					}
					return upstream, nil
				}),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.rule != nil {
				ctx = context.WithValue(ctx, cacheRuleKey{}, tc.rule)
			}
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, target, nil).WithContext(ctx)
			for name, values := range tc.header {
				req.Header[name] = values
			}
			if tc.canceled {
				cancel()
			}
			res, err := transport.RoundTrip(req)
			if res != nil && res.Body != nil {
				defer res.Body.Close()
			}
			if !tc.fallback {
				if res != upstream || (upstream != nil && err != nil) || (upstream == nil && !errors.Is(err, upstreamErr)) {
					t.Fatalf("upstream result replaced: response=%v err=%v", res, err)
				}
				if body.closed.Load() != 0 || logs.Len() != 0 {
					t.Fatalf("upstream result touched: closed=%d logs=%s", body.closed.Load(), logs.String())
				}
				return
			}
			if err != nil || res == nil {
				t.Fatalf("upstream failure not replaced: response=%v err=%v", res, err)
			}
			if res.StatusCode != http.StatusNotModified || res.Status != "304 Not Modified" ||
				res.Proto != "HTTP/1.1" || res.ProtoMajor != 1 || res.ProtoMinor != 1 ||
				res.Body != http.NoBody || res.ContentLength != 0 || res.Request != req {
				t.Fatalf("synthesized response: %+v", res)
			}
			// A validator field in the 304 would have to match the stored entry.
			if _, dateErr := http.ParseTime(res.Header.Get("Date")); dateErr != nil || len(res.Header) != 1 {
				t.Fatalf("synthesized header: %v", res.Header)
			}
			if upstream != nil && !tc.nilBody && body.closed.Load() != 1 {
				t.Fatalf("failed response body closed %d times", body.closed.Load())
			}
			output := logs.String()
			if strings.Count(output, `"level":"warn"`) != 1 || strings.Count(output, "\n") != 1 ||
				!strings.Contains(output, "mirror.example/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb") || strings.Contains(output, "secret") {
				t.Fatalf("fallback log: %s", output)
			}
		})
	}
}

// The fallback wraps the outermost transport, so a refused download admission
// is covered as well as an upstream failure.
func TestImmutableFallbackCoversDownloadAdmission(t *testing.T) {
	resources := guardedResourceManager(t, 8, 32, 1)
	occupied, err := resources.AcquireDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	var upstream atomic.Int64
	ps, err := NewPackageStruct(Options{
		State: newTestState(), Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Resources: resources,
		TransportOverride: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			upstream.Add(1)
			return nil, errors.New("admission must precede the upstream")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string, conditional bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if conditional {
			req.Header.Set("If-Modified-Since", immutableTestModified)
		}
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, req)
		return rec
	}
	if rec := get(immutableTestPackage, true); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("refused immutable revalidation: status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec := get(immutableTestPackage, false); rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("refused unconditional download: status=%d header=%v", rec.Code, rec.Header())
	}
	if rec := get(mutableTestIndex, true); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("refused mutable revalidation: status=%d", rec.Code)
	}
	if upstream.Load() != 0 {
		t.Fatalf("refused downloads reached the upstream %d times", upstream.Load())
	}
}
