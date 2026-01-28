// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	logger "github.com/soulteary/logger-kit"

	apperrors "github.com/soulteary/apt-proxy/internal/errors"
)

// RateLimitMiddleware applies per-IP rate limiting to API handlers.
// When limit is 0, the next handler is called without limiting.
type RateLimitMiddleware struct {
	limitPerMinute int
	mu             sync.Mutex
	buckets        map[string]*rateBucket
	log            *logger.Logger
}

type rateBucket struct {
	count       int
	windowStart time.Time
}

// NewRateLimitMiddleware creates a middleware that allows limitPerMinute requests per IP per minute.
// Pass 0 to disable rate limiting (next handler is returned unchanged).
func NewRateLimitMiddleware(limitPerMinute int, log *logger.Logger) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		limitPerMinute: limitPerMinute,
		buckets:        make(map[string]*rateBucket),
		log:            log,
	}
}

// Wrap wraps an http.Handler with per-IP rate limiting.
// When limit is 0, returns next unchanged. On rate limit exceeded, responds with 429 and ErrRateLimited.
func (m *RateLimitMiddleware) Wrap(next http.Handler) http.Handler {
	if m.limitPerMinute <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if !m.allow(key) {
			if m.log != nil {
				m.log.Warn().
					Str("remote_addr", r.RemoteAddr).
					Str("path", r.URL.Path).
					Msg("API rate limit exceeded")
			}
			WriteAppError(w, apperrors.New(apperrors.ErrRateLimited, "rate limit exceeded"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WrapFunc wraps an http.HandlerFunc with rate limiting.
func (m *RateLimitMiddleware) WrapFunc(next http.HandlerFunc) http.HandlerFunc {
	return m.Wrap(next).ServeHTTP
}

func (m *RateLimitMiddleware) allow(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	b, ok := m.buckets[key]
	if !ok || now.Sub(b.windowStart) >= time.Minute {
		b = &rateBucket{count: 1, windowStart: now}
		m.buckets[key] = b
		return true
	}
	if b.count >= m.limitPerMinute {
		return false
	}
	b.count++
	return true
}

func clientKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' || xff[i] == ' ' {
				return xff[:i]
			}
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
