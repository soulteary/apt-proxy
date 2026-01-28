package errors

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *AppError
		expected string
	}{
		{
			name:     "error without cause",
			err:      New(ErrConfigInvalid, "invalid config"),
			expected: "[CONFIG_INVALID] invalid config",
		},
		{
			name:     "error with cause",
			err:      Wrap(ErrCacheInit, "cache init failed", errors.New("disk full")),
			expected: "[CACHE_INIT_FAILED] cache init failed: disk full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAppError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(ErrInternal, "wrapped error", cause)

	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Test errors.Is with wrapped errors
	if !errors.Is(err, cause) {
		t.Error("errors.Is should return true for wrapped cause")
	}
}

func TestAppError_WithCause(t *testing.T) {
	err := New(ErrInternal, "something failed")
	cause := errors.New("root cause")

	result := err.WithCause(cause)

	if result != err {
		t.Error("WithCause should return the same error instance")
	}
	if err.Cause != cause {
		t.Error("WithCause should set the cause")
	}
}

func TestAppError_WithDetails(t *testing.T) {
	err := New(ErrConfigInvalid, "invalid config")

	result := err.WithDetails("field", "username").WithDetails("reason", "too short")

	if result != err {
		t.Error("WithDetails should return the same error instance")
	}
	if err.Details["field"] != "username" {
		t.Errorf("expected field=username, got %v", err.Details["field"])
	}
	if err.Details["reason"] != "too short" {
		t.Errorf("expected reason=too short, got %v", err.Details["reason"])
	}
}

func TestAppError_WithHTTPStatus(t *testing.T) {
	err := New(ErrInternal, "error")
	result := err.WithHTTPStatus(http.StatusTeapot)

	if result != err {
		t.Error("WithHTTPStatus should return the same error instance")
	}
	if err.HTTPStatus != http.StatusTeapot {
		t.Errorf("expected status %d, got %d", http.StatusTeapot, err.HTTPStatus)
	}
}

func TestAppError_ToJSON(t *testing.T) {
	err := New(ErrConfigInvalid, "invalid config").WithDetails("field", "port")

	data, jsonErr := err.ToJSON()
	if jsonErr != nil {
		t.Fatalf("ToJSON() error = %v", jsonErr)
	}

	var parsed map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal JSON: %v", unmarshalErr)
	}

	if parsed["code"] != string(ErrConfigInvalid) {
		t.Errorf("expected code %s, got %v", ErrConfigInvalid, parsed["code"])
	}
	if parsed["message"] != "invalid config" {
		t.Errorf("expected message 'invalid config', got %v", parsed["message"])
	}
}

func TestNew(t *testing.T) {
	err := New(ErrResourceNotFound, "user not found")

	if err.Code != ErrResourceNotFound {
		t.Errorf("expected code %s, got %s", ErrResourceNotFound, err.Code)
	}
	if err.Message != "user not found" {
		t.Errorf("expected message 'user not found', got %s", err.Message)
	}
	if err.HTTPStatus != http.StatusNotFound {
		t.Errorf("expected HTTP status %d, got %d", http.StatusNotFound, err.HTTPStatus)
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("disk error")
	err := Wrap(ErrCacheWrite, "failed to write cache", cause)

	if err.Code != ErrCacheWrite {
		t.Errorf("expected code %s, got %s", ErrCacheWrite, err.Code)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
	if err.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("expected HTTP status %d, got %d", http.StatusInternalServerError, err.HTTPStatus)
	}
}

func TestCodeToHTTPStatus(t *testing.T) {
	tests := []struct {
		code           Code
		expectedStatus int
	}{
		{ErrConfigInvalid, http.StatusBadRequest},
		{ErrRequestInvalid, http.StatusBadRequest},
		{ErrAuthRequired, http.StatusUnauthorized},
		{ErrAuthInvalid, http.StatusUnauthorized},
		{ErrAuthInsufficient, http.StatusForbidden},
		{ErrResourceNotFound, http.StatusNotFound},
		{ErrMethodNotAllowed, http.StatusMethodNotAllowed},
		{ErrRequestTimeout, http.StatusRequestTimeout},
		{ErrRateLimited, http.StatusTooManyRequests},
		{ErrNotImplemented, http.StatusNotImplemented},
		{ErrProviderDown, http.StatusBadGateway},
		{ErrServerInit, http.StatusServiceUnavailable},
		{ErrInternal, http.StatusInternalServerError},
		{ErrUnknown, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			err := New(tt.code, "test")
			if err.HTTPStatus != tt.expectedStatus {
				t.Errorf("code %s: expected status %d, got %d", tt.code, tt.expectedStatus, err.HTTPStatus)
			}
		})
	}
}

func TestWriteHTTPError(t *testing.T) {
	err := New(ErrResourceNotFound, "user not found").WithDetails("user_id", "123")

	rr := httptest.NewRecorder()
	WriteHTTPError(rr, err)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var resp HTTPErrorResponse
	if unmarshalErr := json.NewDecoder(rr.Body).Decode(&resp); unmarshalErr != nil {
		t.Fatalf("failed to decode response: %v", unmarshalErr)
	}

	if resp.Error != "user not found" {
		t.Errorf("expected error 'user not found', got %s", resp.Error)
	}
	if resp.Code != ErrResourceNotFound {
		t.Errorf("expected code %s, got %s", ErrResourceNotFound, resp.Code)
	}
	if resp.Details["user_id"] != "123" {
		t.Errorf("expected user_id=123, got %v", resp.Details["user_id"])
	}
}

func TestFromHTTPStatus(t *testing.T) {
	tests := []struct {
		status       int
		expectedCode Code
	}{
		{http.StatusBadRequest, ErrRequestInvalid},
		{http.StatusUnauthorized, ErrAuthRequired},
		{http.StatusForbidden, ErrAuthInsufficient},
		{http.StatusNotFound, ErrResourceNotFound},
		{http.StatusMethodNotAllowed, ErrMethodNotAllowed},
		{http.StatusRequestTimeout, ErrRequestTimeout},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusBadGateway, ErrUpstreamError},
		{http.StatusServiceUnavailable, ErrProviderDown},
		{http.StatusInternalServerError, ErrInternal},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			err := FromHTTPStatus(tt.status, "test message")
			if err.Code != tt.expectedCode {
				t.Errorf("status %d: expected code %s, got %s", tt.status, tt.expectedCode, err.Code)
			}
			if err.HTTPStatus != tt.status {
				t.Errorf("expected HTTPStatus %d, got %d", tt.status, err.HTTPStatus)
			}
		})
	}
}

func TestIs(t *testing.T) {
	err := New(ErrConfigInvalid, "config error")

	if !Is(err, ErrConfigInvalid) {
		t.Error("Is should return true for matching code")
	}
	if Is(err, ErrCacheInit) {
		t.Error("Is should return false for non-matching code")
	}
	if Is(errors.New("regular error"), ErrConfigInvalid) {
		t.Error("Is should return false for non-AppError")
	}

	// Wrapped chain: outer wraps inner AppError; Is should find code in chain
	inner := New(ErrCachePurge, "inner purge failed")
	outer := Wrap(ErrInternal, "outer", inner)
	if !Is(outer, ErrCachePurge) {
		t.Error("Is should return true for AppError code in unwrap chain")
	}
	if !Is(outer, ErrInternal) {
		t.Error("Is should return true for outer AppError code")
	}
	if Is(outer, ErrConfigInvalid) {
		t.Error("Is should return false for code not in chain")
	}
}

func TestGetCode(t *testing.T) {
	appErr := New(ErrMirrorTimeout, "timeout")
	regularErr := errors.New("regular error")

	if code := GetCode(appErr); code != ErrMirrorTimeout {
		t.Errorf("expected code %s, got %s", ErrMirrorTimeout, code)
	}
	if code := GetCode(regularErr); code != ErrUnknown {
		t.Errorf("expected code %s for regular error, got %s", ErrUnknown, code)
	}
}

func TestGetHTTPStatus(t *testing.T) {
	appErr := New(ErrResourceNotFound, "not found")
	regularErr := errors.New("regular error")

	if status := GetHTTPStatus(appErr); status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, status)
	}
	if status := GetHTTPStatus(regularErr); status != http.StatusInternalServerError {
		t.Errorf("expected status %d for regular error, got %d", http.StatusInternalServerError, status)
	}
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("root cause")

	t.Run("ConfigError", func(t *testing.T) {
		err := ConfigError("invalid port", cause)
		if err.Code != ErrConfigInvalid {
			t.Errorf("expected code %s, got %s", ErrConfigInvalid, err.Code)
		}
	})

	t.Run("CacheError", func(t *testing.T) {
		err := CacheError(ErrCacheWrite, "write failed", cause)
		if err.Code != ErrCacheWrite {
			t.Errorf("expected code %s, got %s", ErrCacheWrite, err.Code)
		}
	})

	t.Run("MirrorError", func(t *testing.T) {
		err := MirrorError(ErrMirrorUnreachable, "mirror down", cause)
		if err.Code != ErrMirrorUnreachable {
			t.Errorf("expected code %s, got %s", ErrMirrorUnreachable, err.Code)
		}
	})

	t.Run("AuthError", func(t *testing.T) {
		err := AuthError(ErrAuthRequired, "login required")
		if err.Code != ErrAuthRequired {
			t.Errorf("expected code %s, got %s", ErrAuthRequired, err.Code)
		}
		if err.Cause != nil {
			t.Error("AuthError should not have a cause")
		}
	})

	t.Run("ServerError", func(t *testing.T) {
		err := ServerError(ErrServerStart, "failed to start", cause)
		if err.Code != ErrServerStart {
			t.Errorf("expected code %s, got %s", ErrServerStart, err.Code)
		}
	})

	t.Run("InternalError", func(t *testing.T) {
		err := InternalError("unexpected error", cause)
		if err.Code != ErrInternal {
			t.Errorf("expected code %s, got %s", ErrInternal, err.Code)
		}
	})
}
