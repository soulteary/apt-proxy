// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"net"
	"net/http"
	"strings"
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
	// trustedProxies is the optional list of CIDR networks whose remote
	// address is allowed to set X-Forwarded-For. When empty, XFF is ignored
	// (default secure behaviour).
	trustedProxies []*net.IPNet
}

type rateBucket struct {
	count       int
	windowStart time.Time
}

// NewRateLimitMiddleware creates a middleware that allows limitPerMinute requests per IP per minute.
// Pass 0 to disable rate limiting (next handler is returned unchanged).
// trustedProxies is a list of CIDR networks (e.g. "10.0.0.0/8") whose
// X-Forwarded-For header should be honored. Pass nil/empty to ignore XFF entirely.
func NewRateLimitMiddleware(limitPerMinute int, log *logger.Logger, trustedProxies ...string) *RateLimitMiddleware {
	m := &RateLimitMiddleware{
		limitPerMinute: limitPerMinute,
		buckets:        make(map[string]*rateBucket),
		log:            log,
	}
	for _, cidr := range trustedProxies {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			if log != nil {
				log.Warn().Str("cidr", cidr).Err(err).Msg("ignoring invalid trusted proxy CIDR")
			}
			continue
		}
		m.trustedProxies = append(m.trustedProxies, n)
	}
	return m
}

// Wrap wraps an http.Handler with per-IP rate limiting.
// When limit is 0, returns next unchanged. On rate limit exceeded, responds with 429 and ErrRateLimited.
func (m *RateLimitMiddleware) Wrap(next http.Handler) http.Handler {
	if m.limitPerMinute <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := m.clientKey(r)
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
		// Opportunistic GC: when the map grows past a small threshold, prune
		// any buckets whose window has elapsed by more than two minutes.
		if len(m.buckets) > 1024 {
			cutoff := now.Add(-2 * time.Minute)
			for k, v := range m.buckets {
				if v.windowStart.Before(cutoff) {
					delete(m.buckets, k)
				}
			}
		}
		return true
	}
	if b.count >= m.limitPerMinute {
		return false
	}
	b.count++
	return true
}

// clientKey returns the rate-limit key for a request.
// X-Forwarded-For is only honored when the immediate peer (RemoteAddr) is a
// trusted proxy; otherwise the peer's IP is used. This prevents arbitrary
// clients from forging XFF to cycle through limit buckets.
func (m *RateLimitMiddleware) clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if m.isTrustedProxy(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexAny(xff, ", "); i > 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	return host
}

func (m *RateLimitMiddleware) isTrustedProxy(host string) bool {
	if len(m.trustedProxies) == 0 {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range m.trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
