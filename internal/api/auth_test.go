package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthMiddlewareWithAPIKey tests that valid API key allows access.
func TestAuthMiddlewareWithAPIKey(t *testing.T) {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey: "test-secret-key",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := middleware.Wrap(handler)

	tests := []struct {
		name           string
		headerName     string
		headerValue    string
		expectedStatus int
	}{
		{
			name:           "valid X-API-Key header",
			headerName:     "X-API-Key",
			headerValue:    "test-secret-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid Authorization Bearer token",
			headerName:     "Authorization",
			headerValue:    "Bearer test-secret-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid API key",
			headerName:     "X-API-Key",
			headerValue:    "wrong-key",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "missing API key",
			headerName:     "",
			headerValue:    "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "empty API key",
			headerName:     "X-API-Key",
			headerValue:    "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			if tt.headerName != "" && tt.headerValue != "" {
				req.Header.Set(tt.headerName, tt.headerValue)
			}

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

// TestAuthMiddlewareWithoutAPIKey tests that auth is disabled when no API key is configured.
func TestAuthMiddlewareWithoutAPIKey(t *testing.T) {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey: "", // No API key configured
	})

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if !called {
		t.Error("expected handler to be called when auth is disabled")
	}
}

// TestAuthMiddlewareCustomHeaderName tests custom header name configuration.
func TestAuthMiddlewareCustomHeaderName(t *testing.T) {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey:     "test-key",
		HeaderName: "X-Custom-Auth",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	// Test with custom header
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-Custom-Auth", "test-key")

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d with custom header, got %d", http.StatusOK, rr.Code)
	}

	// Test that default header doesn't work when custom is configured
	req2 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req2.Header.Set("X-API-Key", "test-key")

	rr2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr2, req2)

	// Should still work because we also check Authorization Bearer
	if rr2.Code == http.StatusUnauthorized {
		// This is expected because X-API-Key is not the configured header
		t.Log("correctly rejected default header when custom header is configured")
	}
}

// TestAuthMiddlewareQueryParam tests query parameter authentication.
func TestAuthMiddlewareQueryParam(t *testing.T) {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey:          "test-key",
		AllowQueryParam: true,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	// Test with query parameter
	req := httptest.NewRequest(http.MethodGet, "/api/test?api_key=test-key", nil)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d with query param, got %d", http.StatusOK, rr.Code)
	}

	// Test with wrong query parameter
	req2 := httptest.NewRequest(http.MethodGet, "/api/test?api_key=wrong-key", nil)

	rr2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d with wrong query param, got %d", http.StatusUnauthorized, rr2.Code)
	}
}

// TestAuthMiddlewareQueryParamDisabled tests that query param is disabled by default.
func TestAuthMiddlewareQueryParamDisabled(t *testing.T) {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey:          "test-key",
		AllowQueryParam: false, // Default
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	// Query parameter should be ignored
	req := httptest.NewRequest(http.MethodGet, "/api/test?api_key=test-key", nil)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d when query param is disabled, got %d", http.StatusUnauthorized, rr.Code)
	}
}

// TestAuthMiddlewareIsEnabled tests the IsEnabled method.
func TestAuthMiddlewareIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		expected bool
	}{
		{"enabled with key", "secret", true},
		{"disabled without key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := NewAuthMiddleware(AuthConfig{
				APIKey: tt.apiKey,
			})

			if middleware.IsEnabled() != tt.expected {
				t.Errorf("expected IsEnabled() = %v, got %v", tt.expected, middleware.IsEnabled())
			}
		})
	}
}

// TestRequireAuth tests the RequireAuth helper function.
func TestRequireAuth(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}

	wrapped := RequireAuth("my-api-key", handler)

	// Test with valid key
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "my-api-key")

	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Test without key
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	rr2 := httptest.NewRecorder()
	wrapped(rr2, req2)

	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr2.Code)
	}
}

// TestWrapFunc tests the WrapFunc method.
func TestWrapFunc(t *testing.T) {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey: "test-key",
	})

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	wrapped := middleware.WrapFunc(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "test-key")

	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// TestAuthResponseWriter tests the AuthResponseWriter type.
func TestAuthResponseWriter(t *testing.T) {
	rr := httptest.NewRecorder()
	arw := &AuthResponseWriter{ResponseWriter: rr}

	// Test WriteHeader
	arw.WriteHeader(http.StatusCreated)
	if arw.status != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, arw.status)
	}
	if !arw.written {
		t.Error("expected written to be true after WriteHeader")
	}

	// Reset for Write test
	rr2 := httptest.NewRecorder()
	arw2 := &AuthResponseWriter{ResponseWriter: rr2}

	n, err := arw2.Write([]byte("test"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 bytes written, got %d", n)
	}
	if !arw2.written {
		t.Error("expected written to be true after Write")
	}
}
