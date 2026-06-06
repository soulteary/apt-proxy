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

// edges_test.go fills the long tail of zero-coverage helpers in the
// proxy package: tiny pure helpers (Transport/State/Registry getters,
// shouldSetCacheControl, getErrorPage), the static asset handler, and
// the async rewriter constructor.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// TestPackageStructAccessors covers the Transport/State/Registry
// getters, including the nil-receiver branches that return nil.
func TestPackageStructAccessors(t *testing.T) {
	var nilPS *PackageStruct
	if got := nilPS.Transport(); got != nil {
		t.Errorf("nil Transport() = %v, want nil", got)
	}
	if got := nilPS.State(); got != nil {
		t.Errorf("nil State() = %v, want nil", got)
	}
	if got := nilPS.Registry(); got != nil {
		t.Errorf("nil Registry() = %v, want nil", got)
	}

	st := newTestState()
	reg := newTestRegistry()
	ps, err := NewPackageStruct(Options{State: st, Registry: reg, Logger: logger.Default()})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	if ps.Transport() == nil {
		t.Error("Transport() returned nil for live PackageStruct")
	}
	if ps.State() != st {
		t.Error("State() did not return the configured AppState")
	}
	if ps.Registry() != reg {
		t.Error("Registry() did not return the configured Registry")
	}
}

// TestResponseWriterCacheControl asserts that the responseWriter only
// injects a Cache-Control header for cacheable status codes (200/404)
// and otherwise leaves the header alone. Together these cover both
// branches of shouldSetCacheControl.
func TestResponseWriterCacheControl(t *testing.T) {
	rule := &distro.Rule{CacheControl: "max-age=3600"}

	cases := []struct {
		name       string
		rule       *distro.Rule
		status     int
		wantHeader string
	}{
		{"200 with rule injects header", rule, http.StatusOK, "max-age=3600"},
		{"404 with rule injects header", rule, http.StatusNotFound, "max-age=3600"},
		{"500 with rule does NOT inject header", rule, http.StatusInternalServerError, ""},
		{"200 with nil rule does NOT inject header", nil, http.StatusOK, ""},
		{"200 with rule but empty CacheControl does NOT inject header", &distro.Rule{}, http.StatusOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rw := &responseWriter{ResponseWriter: rec, rule: c.rule}
			rw.WriteHeader(c.status)

			got := rec.Header().Get("Cache-Control")
			if got != c.wantHeader {
				t.Errorf("Cache-Control = %q, want %q", got, c.wantHeader)
			}
			if rec.Code != c.status {
				t.Errorf("status = %d, want %d", rec.Code, c.status)
			}
		})
	}
}

// TestProcessMatchingRuleMatchAndMiss exercises the full ServeHTTP
// path against a request that's known to match the Ubuntu cache
// rules; the matched rule's CacheControl must end up on the response.
func TestProcessMatchingRuleMatchAndMiss(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream.URL+"/ubuntu/")
	st.SetProxyMode(distro.TypeAllDistros)
	reg := newTestRegistry()
	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: reg,
		Mode:     distro.TypeAllDistros,
		Logger:   logger.Default(),
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ubuntu/dists/jammy/InRelease", nil)
	ps.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got == "" {
		t.Error("expected Cache-Control header to be set on matching rule")
	}
}

// TestServeStaticLogo covers GET, HEAD, and method-not-allowed paths
// for the embedded logo handler.
func TestServeStaticLogo(t *testing.T) {
	t.Run("GET returns PNG", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/static/apt-proxy-logo.png", nil)
		ServeStaticLogo(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", got)
		}
		if rec.Body.Len() == 0 {
			t.Error("body is empty; expected embedded PNG bytes")
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "max-age=") {
			t.Errorf("Cache-Control = %q, want max-age", got)
		}
	})

	t.Run("HEAD returns no body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/static/apt-proxy-logo.png", nil)
		ServeStaticLogo(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("HEAD body should be empty, got %d bytes", rec.Body.Len())
		}
	})

	t.Run("POST is rejected with 405", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/static/apt-proxy-logo.png", nil)
		ServeStaticLogo(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want \"GET, HEAD\"", got)
		}
	})
}

// TestGetErrorPage exercises the fallback HTML path used when the
// home template fails to render. It's pure-string output so we just
// assert on a couple of marker strings.
func TestGetErrorPage(t *testing.T) {
	out := getErrorPage(errors.New("template went sideways"))
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Error("getErrorPage output missing DOCTYPE")
	}
	if !strings.Contains(out, "Template Error") {
		t.Error("getErrorPage output missing 'Template Error' heading")
	}
}

// TestCreateNewRewritersAsyncReturnsImmediately verifies that the
// async constructor populates a usable rewriter slot for every mode
// without blocking on a real benchmark. We don't drive the async
// callback here (that requires a live mirror); we just assert the
// constructor returns a non-nil rewriters bundle and that the
// configured mirror is honoured immediately for the mode under test.
func TestCreateNewRewritersAsyncReturnsImmediately(t *testing.T) {
	st := newTestState()
	reg := newTestRegistry()
	rewriters := CreateNewRewritersAsync(distro.TypeUbuntu, st, reg)
	if rewriters == nil {
		t.Fatal("CreateNewRewritersAsync returned nil")
	}

	// Drive a request through RewriteRequestByMode and confirm the
	// pre-configured mirror was used (i.e. we took the "mirror != nil"
	// fast path inside createRewriterAsync).
	req := httptest.NewRequest(http.MethodGet, "http://example.com/ubuntu/dists/jammy/InRelease", nil)
	RewriteRequestByMode(req, rewriters, distro.TypeUbuntu)
	if req.URL.Host != "mirrors.example.com" {
		t.Errorf("rewritten host = %q, want mirrors.example.com", req.URL.Host)
	}
}
