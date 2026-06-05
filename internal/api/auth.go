// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	logger "github.com/soulteary/logger-kit"

	apperrors "github.com/soulteary/apt-proxy/internal/errors"
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
	// expectedHash is the SHA-256 hash of the configured API key. We compare
	// the SHA-256 of the incoming key against this digest with ConstantTimeCompare,
	// so the comparison runs over a fixed-length input regardless of the
	// attacker-controlled key length (avoids leaking length via timing).
	expectedHash [sha256.Size]byte
}

// NewAuthMiddleware creates a new AuthMiddleware with the provided configuration.
// If config.HeaderName is empty, it defaults to "X-API-Key".
func NewAuthMiddleware(config AuthConfig) *AuthMiddleware {
	if config.HeaderName == "" {
		config.HeaderName = "X-API-Key"
	}
	m := &AuthMiddleware{config: config}
	if config.APIKey != "" {
		m.expectedHash = sha256.Sum256([]byte(config.APIKey))
	}
	return m
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
			w.Header().Set("WWW-Authenticate", `Bearer realm="apt-proxy"`)
			WriteAppError(w, apperrors.AuthError(apperrors.ErrAuthRequired, "API key required"))
			return
		}

		// Use constant-time SHA-256 digest comparison to prevent timing attacks
		// (digests are fixed-length so the comparison length doesn't depend on
		// attacker input).
		if !m.validateAPIKey(key) {
			m.logAuthFailure(r, "invalid API key")
			w.Header().Set("WWW-Authenticate", `Bearer realm="apt-proxy", error="invalid_token"`)
			WriteAppError(w, apperrors.AuthError(apperrors.ErrAuthInvalid, "Invalid API key"))
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
// Bearer scheme matching is case-insensitive per RFC 7235.
func (m *AuthMiddleware) extractAPIKey(r *http.Request) string {
	// Check header first
	key := r.Header.Get(m.config.HeaderName)
	if key != "" {
		return strings.TrimSpace(key)
	}

	// Check Authorization header with Bearer token (RFC 7235: scheme is
	// case-insensitive, e.g. "Bearer", "bearer", "BEARER").
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Split into scheme + credential without allocating a slice; index of
		// the first space.
		if idx := strings.IndexByte(authHeader, ' '); idx > 0 {
			scheme := authHeader[:idx]
			if strings.EqualFold(scheme, "Bearer") {
				return strings.TrimSpace(authHeader[idx+1:])
			}
		}
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

// validateAPIKey validates the provided API key using SHA-256 + constant-time
// comparison. Hashing first ensures the comparison runs over a fixed-length
// digest rather than a length controlled by the request, so timing cannot
// reveal the configured key's length.
func (m *AuthMiddleware) validateAPIKey(key string) bool {
	got := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(got[:], m.expectedHash[:]) == 1
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
