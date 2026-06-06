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
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	logger "github.com/soulteary/logger-kit"
	tracing "github.com/soulteary/tracing-kit"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

// Default transport timeouts and limits for upstream requests.
// Extract to constants for tuning and documentation.
const (
	DefaultResponseHeaderTimeout = 45 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultMaxIdleConns          = 100
)

// hostPatternEntry pairs a compiled URL pattern with its rules and is used
// instead of map[*regexp.Regexp][]Rule on the request hot path. A slice
// preserves insertion order so the rule selected for a request is
// deterministic across builds (Go map iteration is intentionally randomised).
type hostPatternEntry struct {
	pattern *regexp.Regexp
	rules   []distro.Rule
}

// defaultHostPatterns is the compile-time fallback used when the
// supplied registry has no usable entries (e.g. tests that constructed
// a bare PackageStruct without registering distributions).
var defaultHostPatterns = []hostPatternEntry{
	{pattern: distro.UbuntuHostPattern, rules: distro.UbuntuDefaultCacheRules},
	{pattern: distro.UbuntuPortsHostPattern, rules: distro.UbuntuPortsDefaultCacheRules},
	{pattern: distro.DebianHostPattern, rules: distro.DebianDefaultCacheRules},
	{pattern: distro.CentosHostPattern, rules: distro.CentosDefaultCacheRules},
	{pattern: distro.AlpineHostPattern, rules: distro.AlpineDefaultCacheRules},
}

// hostPatternsFromRegistry materialises the registry's pattern→rules map
// into a stable, ordered slice. Distros are walked via distroModesOrder so
// matching is deterministic for callers that have multiple overlapping rules.
func hostPatternsFromRegistry(reg *distro.Registry) []hostPatternEntry {
	if reg == nil {
		return nil
	}
	all := reg.GetAll()
	out := make([]hostPatternEntry, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, mode := range distroModesOrder {
		for id, d := range all {
			if d.Type != mode || d.URLPattern == nil || len(d.CacheRules) == 0 {
				continue
			}
			out = append(out, hostPatternEntry{pattern: d.URLPattern, rules: d.CacheRules})
			seen[id] = struct{}{}
		}
	}
	for id, d := range all {
		if _, ok := seen[id]; ok {
			continue
		}
		if d.URLPattern == nil || len(d.CacheRules) == 0 {
			continue
		}
		out = append(out, hostPatternEntry{pattern: d.URLPattern, rules: d.CacheRules})
	}
	return out
}

// NewUpstreamTransport constructs a fresh upstream *http.Transport with
// apt-proxy's tuned timeouts and connection-pool defaults.
//
// enableKeepAlive: true reuses connections to mirrors (recommended);
// false disables keep-alives.
func NewUpstreamTransport(enableKeepAlive bool) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
		DisableKeepAlives:     !enableKeepAlive,
		MaxIdleConns:          DefaultMaxIdleConns,
		IdleConnTimeout:       DefaultIdleConnTimeout,
		DisableCompression:    false,
	}
}

// PackageStruct is the main HTTP handler that routes requests to appropriate
// distribution-specific handlers and applies caching rules. It owns all of
// the per-Server state previously held in package-level globals: the
// AppState, distro Registry, URL rewriters, and the host-pattern cache.
type PackageStruct struct {
	Handler  http.Handler   // The underlying HTTP handler (typically a reverse proxy)
	Rules    []distro.Rule  // Caching rules for different package types
	CacheDir string         // Cache directory path for statistics
	log      *logger.Logger // Structured logger

	state    *state.AppState
	registry *distro.Registry
	mode     int

	// rewriters holds the URL rewriters used by ServeHTTP. Writers swap
	// the pointer under refreshMu; the URLRewriters struct itself has
	// finer-grained locking for the per-mirror pointer swap.
	rewriters *URLRewriters

	// bench is this PackageStruct's private benchmark engine. Each Server
	// owns one so RefreshMirrors on Server A no longer flushes Server B's
	// mirror selection cache (the long-standing cross-Server coupling
	// previously pinned by tests/integration/multi_server_test.go).
	bench *benchmarks.Engine

	// transport is the upstream HTTP transport (with retry+tracing wrapping)
	// used by the underlying ReverseProxy.
	transport http.RoundTripper

	// hostPatternCache caches the snapshot of registry-derived host
	// patterns so we don't allocate/copy on every request. RefreshMirrors
	// clears this pointer; readers fall back to defaultHostPatterns when
	// the registry returns an empty result.
	hostPatternCache atomic.Pointer[[]hostPatternEntry]

	// refreshMu serializes RefreshMirrors so two concurrent reload paths
	// (SIGHUP debounced reload + /api/mirrors/refresh) don't race when
	// rebuilding rewriters. Readers don't take this mutex.
	refreshMu sync.Mutex
}

// Options configures NewPackageStruct.
type Options struct {
	State             *state.AppState
	Registry          *distro.Registry
	CacheDir          string
	Logger            *logger.Logger
	Mode              int
	EnableKeepAlive   bool
	Async             bool              // when true, use async (non-blocking) benchmarks during construction
	TransportOverride http.RoundTripper // optional: caller-supplied transport (mainly for tests)
}

// NewPackageStruct constructs a fully wired PackageStruct using the
// supplied state/registry. State and Registry are required; Logger
// defaults to logger.Default() when nil.
func NewPackageStruct(opts Options) (*PackageStruct, error) {
	if opts.State == nil {
		return nil, errors.New("proxy: Options.State is required")
	}
	if opts.Registry == nil {
		return nil, errors.New("proxy: Options.Registry is required")
	}

	log := opts.Logger
	if log == nil {
		log = logger.Default()
	}

	transport := opts.TransportOverride
	if transport == nil {
		transport = NewRetryableTransport(NewUpstreamTransport(opts.EnableKeepAlive))
	}

	mode := opts.Mode
	bench := benchmarks.NewEngine()
	rewriters := newRewriters(mode, opts.State, opts.Registry, opts.Async, bench)

	ps := &PackageStruct{
		Rules:     GetRewriteRulesByMode(opts.Registry, mode),
		CacheDir:  opts.CacheDir,
		log:       log,
		state:     opts.State,
		registry:  opts.Registry,
		mode:      mode,
		rewriters: rewriters,
		bench:     bench,
		transport: transport,
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: transport,
		},
	}
	return ps, nil
}

// newRewriters chooses the sync/async constructor based on opts.Async.
func newRewriters(mode int, st *state.AppState, reg *distro.Registry, async bool, bench *benchmarks.Engine) *URLRewriters {
	if async {
		return CreateNewRewritersAsyncWithEngine(mode, st, reg, bench)
	}
	return CreateNewRewritersWithEngine(mode, st, reg, bench)
}

// HandleHomePage serves the home page with statistics
func HandleHomePage(rw http.ResponseWriter, r *http.Request, cacheDir string) {
	tpl, status := RenderInternalUrls("/", cacheDir)
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	if _, err := io.WriteString(rw, tpl); err != nil {
		logger.Default().Error().Err(err).Msg("Error rendering home page")
	}
}

// Transport returns the upstream HTTP transport. Exposed mainly so the
// caller can wrap it (e.g. with httpcache) when constructing the final
// handler chain.
func (ap *PackageStruct) Transport() http.RoundTripper {
	if ap == nil {
		return nil
	}
	return ap.transport
}

// State returns the AppState backing this PackageStruct (read-only access
// for callers that need to inspect or mutate mirror configuration).
func (ap *PackageStruct) State() *state.AppState {
	if ap == nil {
		return nil
	}
	return ap.state
}

// Registry returns the distribution registry backing this PackageStruct.
func (ap *PackageStruct) Registry() *distro.Registry {
	if ap == nil {
		return nil
	}
	return ap.registry
}

// ServeHTTP implements http.Handler interface. It processes incoming requests,
// matches them against caching rules, and routes them to the appropriate handler.
// If a matching rule is found, the request is processed with cache control headers.
func (ap *PackageStruct) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	spanCtx, span := tracing.StartSpan(ctx, "proxy.request")
	defer span.End()

	tracing.SetSpanAttributesFromMap(span, map[string]interface{}{
		"http.method":      r.Method,
		"http.url":         r.URL.String(),
		"http.path":        r.URL.Path,
		"http.scheme":      r.URL.Scheme,
		"http.host":        r.Host,
		"http.user_agent":  r.UserAgent(),
		"http.remote_addr": r.RemoteAddr,
	})

	r = r.WithContext(spanCtx)

	rule := ap.handleExternalURLs(r)
	if rule != nil {
		if name := distro.DistributionName(rule.OS); name != "" {
			tracing.SetSpanAttributes(span, map[string]string{
				"proxy.distribution": name,
			})
		}

		if ap.Handler != nil {
			ap.Handler.ServeHTTP(&responseWriter{rw, rule}, r)
		} else {
			tracing.RecordError(span, http.ErrAbortHandler)
			http.Error(rw, "Internal Server Error: handler not initialized", http.StatusInternalServerError)
		}
	} else {
		tracing.SetSpanAttributes(span, map[string]string{
			"http.status_code": "404",
		})
		http.NotFound(rw, r)
	}
}

// responseWriter wraps http.ResponseWriter to inject cache control headers
// based on the matched caching rule.
type responseWriter struct {
	http.ResponseWriter
	rule *distro.Rule // The matched caching rule for this request
}

// hostPatterns returns this PackageStruct's cached pattern→rules entries,
// populating the cache on first access. The fallback (defaultHostPatterns)
// is used only if the registry has nothing to offer.
func (ap *PackageStruct) hostPatterns() []hostPatternEntry {
	if cur := ap.hostPatternCache.Load(); cur != nil && len(*cur) > 0 {
		return *cur
	}
	entries := hostPatternsFromRegistry(ap.registry)
	if len(entries) == 0 {
		return defaultHostPatterns
	}
	ap.hostPatternCache.Store(&entries)
	return entries
}

// invalidateHostPatterns forces the next request to rebuild the cache
// from the registry. Called from RefreshMirrors / SIGHUP reload.
func (ap *PackageStruct) invalidateHostPatterns() {
	ap.hostPatternCache.Store(nil)
}

// handleExternalURLs processes requests for external package repositories.
// It matches the request path against known distribution patterns and returns
// the appropriate caching rule if a match is found.
func (ap *PackageStruct) handleExternalURLs(r *http.Request) *distro.Rule {
	path := r.URL.Path
	for _, entry := range ap.hostPatterns() {
		if entry.pattern.MatchString(path) {
			return ap.processMatchingRule(r, entry.rules)
		}
	}
	return nil
}

// processMatchingRule processes a request that matches a distribution pattern.
// It finds the specific caching rule, removes client cache control headers,
// and rewrites the URL if necessary.
func (ap *PackageStruct) processMatchingRule(r *http.Request, rules []distro.Rule) *distro.Rule {
	rule, match := MatchingRule(r.URL.Path, rules)
	if !match {
		return nil
	}

	r.Header.Del("Cache-Control")
	if rule.Rewrite {
		ap.rewriteRequest(r, rule)
	}
	return rule
}

// rewriteRequest rewrites the request URL to point to the configured mirror
// for the distribution. This enables transparent proxying to different mirrors
// while maintaining the original request path structure.
func (ap *PackageStruct) rewriteRequest(r *http.Request, rule *distro.Rule) {
	if r.URL == nil {
		ap.log.Error().Msg("request URL is nil, cannot rewrite")
		return
	}
	before := r.URL.String()
	RewriteRequestByMode(r, ap.rewriters, rule.OS)

	if r.URL != nil {
		r.Host = r.URL.Host
		ap.log.Debug().
			Str("from", before).
			Str("to", r.URL.String()).
			Msg("rewrote request URL")
	}
}

// RefreshMirrors refreshes this PackageStruct's mirror configuration.
// Triggered by SIGHUP and POST /api/mirrors/refresh. The mutex serializes
// concurrent refreshes (the rewriter pointer swap inside RefreshRewriters
// has its own finer-grained lock; this outer lock prevents two refresh
// runs from racing to clear the benchmark cache and re-elect mirrors at
// the same time).
//
// Cache-isolation note: this clears only this PackageStruct's private
// benchmarks.Engine cache, so a refresh on one Server no longer affects
// any other Server's mirror selection. (Historically the engine was a
// package-level singleton; that coupling was removed in favour of the
// per-Server engine field above.)
func (ap *PackageStruct) RefreshMirrors() {
	if ap == nil || ap.rewriters == nil {
		return
	}
	ap.refreshMu.Lock()
	defer ap.refreshMu.Unlock()
	ap.invalidateHostPatterns()
	RefreshRewritersWithEngine(ap.rewriters, ap.mode, ap.state, ap.registry, ap.bench)
}

// BenchmarkEngine exposes this PackageStruct's private benchmark engine.
// Callers (tests, debug endpoints) should prefer this over
// benchmarks.Default() so they observe the same cache the Server uses.
func (ap *PackageStruct) BenchmarkEngine() *benchmarks.Engine {
	if ap == nil {
		return nil
	}
	return ap.bench
}

// WriteHeader implements http.ResponseWriter interface. It injects cache control
// headers based on the matched rule before writing the status code.
func (rw *responseWriter) WriteHeader(status int) {
	if rw.shouldSetCacheControl(status) {
		rw.Header().Set("Cache-Control", rw.rule.CacheControl)
	}
	rw.ResponseWriter.WriteHeader(status)
}

// shouldSetCacheControl determines whether cache control headers should be set
// for the given HTTP status code. Only certain status codes are cacheable.
func (rw *responseWriter) shouldSetCacheControl(status int) bool {
	return rw.rule != nil &&
		rw.rule.CacheControl != "" &&
		(status == http.StatusOK || status == http.StatusNotFound)
}
