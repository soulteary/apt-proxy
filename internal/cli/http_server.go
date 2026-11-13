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

package cli

import (
	"context"
	"crypto/tls"
	stderrors "errors"
	"io"
	stdlog "log"
	"net"
	"net/http"
	"strings"
	"time"

	health "github.com/soulteary/health-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
	metrics "github.com/soulteary/metrics-kit/v3"
	middleware "github.com/soulteary/middleware-kit/v3"
	version "github.com/soulteary/version-kit/v4"
	versionhttp "github.com/soulteary/version-kit/v4/httpadapter"

	"github.com/soulteary/apt-proxy/internal/api"
	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/internal/logsafe"
	"github.com/soulteary/apt-proxy/internal/proxy"
)

// HTTP server limits.
const (
	// defaultReadTimeout bounds reading the request line and headers.
	defaultReadTimeout = 50 * time.Second
	// Package downloads may legitimately take hours on slow links. A whole-
	// response write deadline terminates healthy streaming transfers, so rely
	// on the idle timeout and request cancellation instead.
	defaultWriteTimeout   = 0
	defaultIdleTimeout    = 120 * time.Second
	defaultMaxHeaderBytes = 4096 * 4
)

// cacheLabelFromHeader normalizes X-Cache header to HIT/MISS/SKIP for logging.
func cacheLabelFromHeader(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return "SKIP"
	}
	if strings.HasPrefix(h, "HIT") {
		return "HIT"
	}
	if strings.HasPrefix(h, "MISS") {
		return "MISS"
	}
	return h
}

// route is one operational or management endpoint.
type route struct {
	handler http.Handler
	// anyMethod is false for endpoints that answer GET and HEAD only. Other
	// methods fall through to the repository handler, which rejects them.
	anyMethod bool
}

// newHandler builds the handler for every request: operational endpoints, the
// management API and, for all remaining paths, the caching repository proxy.
func (s *Server) newHandler() http.Handler {
	// Read-only statistics may be exposed without a key for compatibility, but
	// mutating management routes fail closed unless authentication is configured.
	apiHandler := func(h http.HandlerFunc) http.Handler {
		return s.rateLimitMiddleware.Wrap(s.authMiddleware.WrapFunc(h))
	}
	mutatingAPIHandler := func(h http.HandlerFunc) http.Handler {
		if !s.authMiddleware.IsEnabled() {
			return s.rateLimitMiddleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				api.WriteAppError(w, apperrors.New(
					apperrors.ErrAuthInsufficient,
					"management API is disabled until API authentication is configured",
				).WithHTTPStatus(http.StatusServiceUnavailable))
			}))
		}
		return apiHandler(h)
	}

	healthz := health.Handler(s.healthAggregator)
	ping := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "pong")
	})
	routes := map[string]route{
		"/healthz": {handler: healthz},
		"/livez":   {handler: health.LivenessHandler("apt-proxy")},
		"/readyz":  {handler: healthz},
		"/version": {handler: versionhttp.Handler(version.HandlerConfig{Info: s.versionInfo, Pretty: true})},
		"/metrics": {handler: metrics.HandlerFor(s.metricsRegistry)},
		"/": {handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxy.HandleHomePage(w, r, s.config.CacheDir)
		})},
		"/static/apt-proxy-logo.png": {handler: http.HandlerFunc(proxy.ServeStaticLogo)},

		"/_/ping":              {handler: ping, anyMethod: true},
		"/api/cache/stats":     {handler: apiHandler(s.cacheHandler.HandleCacheStats), anyMethod: true},
		"/api/cache/purge":     {handler: mutatingAPIHandler(s.cacheHandler.HandleCachePurge), anyMethod: true},
		"/api/cache/cleanup":   {handler: mutatingAPIHandler(s.cacheHandler.HandleCacheCleanup), anyMethod: true},
		"/api/mirrors/refresh": {handler: mutatingAPIHandler(s.mirrorsHandler.HandleMirrorsRefresh), anyMethod: true},
		"/api/mirrors/status":  {handler: apiHandler(s.handleMirrorStatus), anyMethod: true},
	}

	// Access is checked before routing, cache lookup or upstream access.
	repository := proxy.GuardRequestPath(s.downloadClients.Wrap(s.proxy))

	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := routePath(r.URL.Path)
		if strings.HasPrefix(path, "/_/ping/") {
			ping.ServeHTTP(w, r)
			return
		}
		if matched, ok := routes[path]; ok && (matched.anyMethod || r.Method == http.MethodGet || r.Method == http.MethodHead) {
			matched.handler.ServeHTTP(w, r)
			return
		}
		repository.ServeHTTP(w, r)
	})

	var handler http.Handler = router
	handler = s.accessLog(handler)
	handler = middleware.SecurityHeadersStd(middleware.DefaultSecurityHeadersConfig())(handler)
	handler = versionhttp.Middleware(s.versionInfo, "X-")(handler)
	return handler
}

// routePath is the lookup key for operational and management endpoints. They
// match without regard to case or one trailing slash.
func routePath(path string) string {
	path = strings.ToLower(path)
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func (s *Server) handleMirrorStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		api.WriteAppError(w, apperrors.New(apperrors.ErrMethodNotAllowed, "Method not allowed"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := api.WriteJSON(w, http.StatusOK, s.proxy.MirrorHealthStatus()); err != nil {
		s.log.Error().Err(err).Msg("failed to write mirror health status")
	}
}

type responseHeaderKey struct{}

// accessLog writes one line per request. The cache outcome is read from the
// response headers after the handler has finished.
func (s *Server) accessLog(next http.Handler) http.Handler {
	cfg := logger.DefaultMiddlewareConfig()
	cfg.Logger = s.log
	cfg.SkipPaths = []string{"/healthz", "/livez", "/readyz"} // skip health noise
	// Repository authentication can use custom query names. Add a uniformly
	// redacted query field below instead of the logger's fixed secret-name list.
	cfg.IncludeQuery = false
	if s.config.Debug {
		cfg.IncludeHeaders = true
		cfg.IncludeBody = true
	}
	cfg.CustomFields = func(r *http.Request) map[string]interface{} {
		fields := make(map[string]interface{}, 2)
		if header, ok := r.Context().Value(responseHeaderKey{}).(http.Header); ok {
			fields["cache"] = cacheLabelFromHeader(header.Get("X-Cache"))
		}
		if query := r.URL.RawQuery; query != "" {
			fields["query"] = logsafe.Query(query)
		}
		return fields
	}
	logged := logger.Middleware(cfg)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), responseHeaderKey{}, w.Header())
		logged.ServeHTTP(w, r.WithContext(ctx))
	})
}

// newHTTPServer configures the listener-independent part of the server.
// Requests inherit a context that is cancelled when shutdown begins, so an
// upstream fetch stalled on a mirror does not hold the drain open.
func (s *Server) newHTTPServer() *http.Server {
	requests, stop := context.WithCancel(context.Background())
	s.stopRequests = stop
	server := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }),
		ReadHeaderTimeout: defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return requests },
		ErrorLog:          stdlog.New(serverLogWriter{s}, "", 0),
		// HTTP/1.1 only, as before. A non-nil empty map keeps net/http from
		// enabling HTTP/2 on TLS listeners.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
	}
	server.RegisterOnShutdown(stop)
	return server
}

// serverLogWriter forwards net/http's own diagnostics, including the stack of
// a recovered handler panic, to the structured logger.
type serverLogWriter struct{ server *Server }

func (w serverLogWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if strings.HasPrefix(message, "http: panic serving") {
		w.server.log.Error().Msg(message)
	} else {
		w.server.log.Warn().Msg(message)
	}
	return len(p), nil
}

// listen serves on l until the server shuts down, which is not an error.
func (s *Server) listen(l net.Listener) error {
	var err error
	if s.config.TLS.Enabled {
		err = s.httpServer.ServeTLS(l, s.config.TLS.CertFile, s.config.TLS.KeyFile)
	} else {
		err = s.httpServer.Serve(l)
	}
	if stderrors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
