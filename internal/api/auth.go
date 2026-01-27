// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	logger "github.com/soulteary/logger-kit"
)

// AuthConfig holds authentication configuration for the API middleware.
type AuthConfig struct {
	// APIKey is the required API key for accessing protected endpoints.
	// If empty, authentication is disabled.
	APIKey string

	// HeaderName is the HTTP header name to read the API key from.
	// Defaults to "X-API-Key" if not specified.
	HeaderName string

	// AllowQueryParam allows API key to be passed as a query parameter.
	// The parameter name will be "api_key". Default is false for security.
	AllowQueryParam bool

	// Logger is the structured logger for authentication events.
	Logger *logger.Logger
}

// AuthMiddleware provides API key authentication for protected endpoints.
type AuthMiddleware struct {
	config AuthConfig
}

// NewAuthMiddleware creates a new AuthMiddleware with the provided configuration.
// If config.HeaderName is empty, it defaults to "X-API-Key".
func NewAuthMiddleware(config AuthConfig) *AuthMiddleware {
	if config.HeaderName == "" {
		config.HeaderName = "X-API-Key"
	}
	return &AuthMiddleware{
		config: config,
	}
}

// Wrap wraps an http.Handler with API key authentication.
// If no API key is configured, the handler is returned without modification.
// Returns 401 Unauthorized if authentication fails.
func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	// If no API key is configured, skip authentication
	if m.config.APIKey == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := m.extractAPIKey(r)

		if key == "" {
			m.logAuthFailure(r, "missing API key")
			WriteJSONError(w, http.StatusUnauthorized, "API key required")
			return
		}

		// Use constant-time comparison to prevent timing attacks
		if !m.validateAPIKey(key) {
			m.logAuthFailure(r, "invalid API key")
			WriteJSONError(w, http.StatusUnauthorized, "Invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// WrapFunc wraps an http.HandlerFunc with API key authentication.
func (m *AuthMiddleware) WrapFunc(next http.HandlerFunc) http.HandlerFunc {
	return m.Wrap(next).ServeHTTP
}

// extractAPIKey extracts the API key from the request.
// It checks the configured header first, then the query parameter if allowed.
func (m *AuthMiddleware) extractAPIKey(r *http.Request) string {
	// Check header first
	key := r.Header.Get(m.config.HeaderName)
	if key != "" {
		return strings.TrimSpace(key)
	}

	// Check Authorization header with Bearer token
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	// Check query parameter if allowed
	if m.config.AllowQueryParam {
		key = r.URL.Query().Get("api_key")
		if key != "" {
			return strings.TrimSpace(key)
		}
	}

	return ""
}

// validateAPIKey validates the provided API key using constant-time comparison.
func (m *AuthMiddleware) validateAPIKey(key string) bool {
	return subtle.ConstantTimeCompare([]byte(key), []byte(m.config.APIKey)) == 1
}

// logAuthFailure logs an authentication failure event.
func (m *AuthMiddleware) logAuthFailure(r *http.Request, reason string) {
	if m.config.Logger != nil {
		m.config.Logger.Warn().
			Str("path", r.URL.Path).
			Str("method", r.Method).
			Str("remote_addr", r.RemoteAddr).
			Str("reason", reason).
			Msg("API authentication failed")
	}
}

// IsEnabled returns true if authentication is enabled (API key is configured).
func (m *AuthMiddleware) IsEnabled() bool {
	return m.config.APIKey != ""
}

// RequireAuth is a middleware function that can be used with standard http.ServeMux.
// It wraps a handler function with authentication.
func RequireAuth(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	middleware := NewAuthMiddleware(AuthConfig{
		APIKey: apiKey,
	})
	return middleware.WrapFunc(next)
}

// AuthResponseWriter wraps http.ResponseWriter to track if a response has been written.
type AuthResponseWriter struct {
	http.ResponseWriter
	written bool
	status  int
}

// WriteHeader captures the status code and marks the response as written.
func (w *AuthResponseWriter) WriteHeader(status int) {
	w.status = status
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}

// Write marks the response as written and writes the data.
func (w *AuthResponseWriter) Write(data []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(data)
}
