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
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	logger "github.com/soulteary/logger-kit/v3"
	tracing "github.com/soulteary/tracing-kit/v2"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/passthrough"
	"github.com/soulteary/apt-proxy/internal/state"
)

// Default transport timeouts and limits for upstream requests.
// Extract to constants for tuning and documentation.
const (
	DefaultResponseHeaderTimeout = 45 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultMaxIdleConns          = 100
)

func detachRequest(ctx context.Context, r *http.Request) *http.Request {
	detached := r.Clone(ctx)
	detached.Method = strings.Clone(detached.Method)
	detached.Host = strings.Clone(detached.Host)
	detached.RemoteAddr = strings.Clone(detached.RemoteAddr)
	detached.RequestURI = strings.Clone(detached.RequestURI)

	if detached.URL != nil {
		u := *detached.URL
		u.Scheme = strings.Clone(u.Scheme)
		u.Opaque = strings.Clone(u.Opaque)
		u.Host = strings.Clone(u.Host)
		u.Path = strings.Clone(u.Path)
		u.RawPath = strings.Clone(u.RawPath)
		u.RawQuery = strings.Clone(u.RawQuery)
		u.Fragment = strings.Clone(u.Fragment)
		u.RawFragment = strings.Clone(u.RawFragment)
		if u.User != nil {
			username := strings.Clone(u.User.Username())
			if password, ok := u.User.Password(); ok {
				u.User = url.UserPassword(username, strings.Clone(password))
			} else {
				u.User = url.User(username)
			}
		}
		detached.URL = &u
	}

	detached.Header = detachHeader(detached.Header)
	detached.Trailer = detachHeader(detached.Trailer)
	for i := range detached.TransferEncoding {
		detached.TransferEncoding[i] = strings.Clone(detached.TransferEncoding[i])
	}
	return detached
}

func detachHeader(header http.Header) http.Header {
	if header == nil {
		return nil
	}
	detached := make(http.Header, len(header))
	for key, values := range header {
		cloned := make([]string, len(values))
		for i, value := range values {
			cloned[i] = strings.Clone(value)
		}
		detached[strings.Clone(key)] = cloned
	}
	return detached
}

// hostPatternEntry pairs a compiled URL pattern with its rules and is used
// instead of map[*regexp.Regexp][]Rule on the request hot path. A slice
// preserves insertion order so the rule selected for a request is
// deterministic across builds (Go map iteration is intentionally randomised).
type hostPatternEntry struct {
	pattern *regexp.Regexp
	// hostPattern, when set, matches the request's Host header for archives
	// served from the host root. It is tried only after pattern fails.
	hostPattern *regexp.Regexp
	rules       []distro.Rule
}

// defaultHostPatterns is the compile-time fallback used when the
// supplied registry has no usable entries (e.g. tests that constructed
// a bare PackageStruct without registering distributions).
var defaultHostPatterns = []hostPatternEntry{
	{pattern: distro.UbuntuHostPattern, rules: distro.UbuntuDefaultCacheRules},
	{pattern: distro.UbuntuPortsHostPattern, rules: distro.UbuntuPortsDefaultCacheRules},
	{pattern: distro.DebianHostPattern, hostPattern: distro.DebianSecurityHostPattern, rules: distro.DebianDefaultCacheRules},
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
			out = append(out, hostPatternEntry{pattern: d.URLPattern, hostPattern: d.HostPattern, rules: d.CacheRules})
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
		out = append(out, hostPatternEntry{pattern: d.URLPattern, hostPattern: d.HostPattern, rules: d.CacheRules})
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
// routing is the pair of registry-derived structures a request depends on:
// the host patterns it matches against, and the rewriters that turn a match
// into an upstream URL. They are published together because publishing them
// separately opens a window that 502s.
//
// The old code invalidated the host-pattern cache first and then rebuilt the
// rewriters synchronously, benchmarking mirrors as it went. For the length of
// that rebuild -- network-bound, so not short -- a request matched the *new*
// rules while RewriteRequestByMode still held the *old* rewriter set. A newly
// added distribution had no rewriter at all, and one whose url_pattern had
// changed matched on the new pattern but rewrote with the old one. Either way
// the URL stayed relative and the reverse proxy answered 502.
//
// Reversing the order only mirrors the window. The fix is to build both sides
// before either goes live, which is what RefreshMirrors now does.
type routing struct {
	patterns  []hostPatternEntry
	rewriters *URLRewriters
}

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

	// passthrough is the allowlist of third-party origins served unrewritten.
	// Empty by default: apt-proxy is not an open forward proxy.
	passthrough *passthrough.List

	// routing holds the derived pair ServeHTTP reads: the host patterns a
	// request matches on, and the rewriters it is handed once matched. They
	// live behind one pointer because they must agree -- see the routing
	// type. Writers build a replacement and Store it under refreshMu;
	// readers Load once per request. The URLRewriters struct keeps its own
	// finer-grained lock for the async per-mirror pointer swap.
	routing atomic.Pointer[routing]

	// bench is this PackageStruct's private benchmark engine. Each Server
	// owns one so RefreshMirrors on Server A no longer flushes Server B's
	// mirror selection cache (the long-standing cross-Server coupling
	// previously pinned by tests/integration/multi_server_test.go).
	bench *benchmarks.Engine

	// transport is the upstream HTTP transport (with retry+tracing wrapping)
	// used by the underlying ReverseProxy.
	transport http.RoundTripper

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
	Passthrough       *passthrough.List // optional: allowlisted third-party origins
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
		Rules:       GetRewriteRulesByMode(opts.Registry, mode),
		CacheDir:    opts.CacheDir,
		log:         log,
		state:       opts.State,
		registry:    opts.Registry,
		mode:        mode,
		passthrough: opts.Passthrough,
		bench:       bench,
		transport:   transport,
		Handler: &httputil.ReverseProxy{
			Rewrite:   func(*httputil.ProxyRequest) {},
			Transport: transport,
		},
	}
	ps.routing.Store(&routing{
		patterns:  hostPatternsFromRegistry(opts.Registry),
		rewriters: rewriters,
	})
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

// tlsRewriteMarker is apt-cacher-ng's "tell-me-what-you-need" marker. A client
// writes the upstream as http://HTTPS///<host>/... and the proxy is expected to
// fetch https://<host>/... on its behalf.
const tlsRewriteMarker = "HTTPS//"

// parseTLSRewriteMarker pulls the origin and upstream path out of a marker URL,
// in either spelling apt-cacher-ng documents:
//
//	deb http://HTTPS///get.docker.com/ubuntu ...           -> Host "HTTPS", path "///get.docker.com/..."
//	deb http://proxy:3142/HTTPS///get.docker.com/ubuntu ... -> path "/HTTPS///get.docker.com/..."
//
// Recognising it is not optional even when it will be refused: such a path
// still contains a distribution segment (".../ubuntu/dists/..."), so letting it
// reach pattern matching would rewrite a request meant for a third-party host
// onto a Ubuntu/Debian mirror.
//
// found reports that the request carries a marker at all, which is what
// separates "not a marker request" from "a marker request we could not parse".
func parseTLSRewriteMarker(r *http.Request) (origin, upstreamPath string, found bool) {
	if r == nil || r.URL == nil {
		return "", "", false
	}

	escaped := r.URL.EscapedPath()
	var rest string

	// The authority lands in URL.Host for an absolute-form request, but Fiber's
	// adaptor converts to a server request where it lives in Host and URL.Host
	// is empty. Production traffic takes the latter path, so check both.
	for _, authority := range [2]string{r.URL.Host, r.Host} {
		if authority == "" {
			continue
		}
		host := authority
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if strings.EqualFold(host, "HTTPS") {
			rest = escaped
			found = true
			break
		}
	}

	if !found {
		upper := strings.ToUpper(escaped)
		marker := "/" + tlsRewriteMarker
		if idx := strings.Index(upper, marker); idx >= 0 {
			rest = escaped[idx+len(marker):]
			found = true
		}
	}
	if !found {
		return "", "", false
	}

	// Whatever spelling got us here, the origin is the first path segment left.
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return "", "", true
	}
	origin = rest
	upstreamPath = "/"
	if cut := strings.IndexByte(rest, '/'); cut >= 0 {
		origin, upstreamPath = rest[:cut], rest[cut:]
	}
	if unescaped, err := url.PathUnescape(origin); err == nil {
		origin = unescaped
	}
	return origin, upstreamPath, true
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

	// Fiber/fasthttp reuses its request buffers after ServeHTTP returns, while
	// the cache may finish a write asynchronously and retain the request URL as
	// part of its key. Clone here so background cache work never references
	// memory that the adapter can reuse for the next request.
	r = detachRequest(spanCtx, r)

	// apt-cacher-ng's HTTPS/// marker names a third-party origin inside the
	// path. Handle it here rather than letting it reach pattern matching,
	// which would rewrite it onto a distribution mirror.
	if origin, upstreamPath, found := parseTLSRewriteMarker(r); found {
		rule := ap.acceptTLSRewriteMarker(rw, r, origin, upstreamPath, span)
		if rule == nil {
			return // the refusal has been written
		}
		if ap.Handler == nil {
			tracing.RecordError(span, http.ErrAbortHandler)
			http.Error(rw, "Internal Server Error: handler not initialized", http.StatusInternalServerError)
			return
		}
		ap.Handler.ServeHTTP(&responseWriter{rw, rule}, r)
		return
	}

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

// currentRouting returns the snapshot a single request should serve from.
// Callers Load once and thread the result through matching and rewriting, so
// a refresh landing mid-request cannot hand them a pattern from one snapshot
// and a rewriter from the next.
//
// The registry can still be empty here -- a PackageStruct built before its
// registry was populated -- so an empty snapshot re-derives the patterns and
// republishes them against the same rewriters. CompareAndSwap makes that
// retry lose to a concurrent RefreshMirrors rather than overwrite it.
func (ap *PackageStruct) currentRouting() *routing {
	cur := ap.routing.Load()
	if cur == nil {
		return &routing{patterns: defaultHostPatterns}
	}
	if len(cur.patterns) > 0 {
		return cur
	}

	entries := hostPatternsFromRegistry(ap.registry)
	if len(entries) == 0 {
		return &routing{patterns: defaultHostPatterns, rewriters: cur.rewriters}
	}
	next := &routing{patterns: entries, rewriters: cur.rewriters}
	if ap.routing.CompareAndSwap(cur, next) {
		return next
	}
	return ap.routing.Load()
}

// handleExternalURLs processes requests for external package repositories.
// It matches the request path against known distribution patterns and returns
// the appropriate caching rule if a match is found.
func (ap *PackageStruct) handleExternalURLs(r *http.Request) *distro.Rule {
	path := r.URL.Path

	// One Load for the whole request. Matching on this snapshot's patterns
	// and rewriting with its rewriters is what keeps the two consistent
	// while a refresh publishes a replacement underneath us.
	rt := ap.currentRouting()
	entries := rt.patterns

	// Path match first: it is the common case and the more specific signal,
	// so an archive reachable by path keeps its existing routing even when
	// some other distro claims the same host. matchesDistroPath additionally
	// rejects a match hiding behind a path prefix that is not a mirror host,
	// which is what keeps a third-party archive (a PPA, a vendor repo) from
	// being answered out of the distribution's own mirror -- see hostprefix.go.
	for _, entry := range entries {
		if matchesDistroPath(entry.pattern, path) {
			return ap.processMatchingRule(r, rt, entry.rules)
		}
	}

	// Fall back to the Host header for archives served from the host root,
	// where no path prefix exists for the URL pattern to match.
	host := requestHost(r)
	if host == "" {
		return nil
	}
	for _, entry := range entries {
		if entry.hostPattern != nil && entry.hostPattern.MatchString(host) {
			return ap.processMatchingRule(r, rt, entry.rules)
		}
	}

	// Last: a third-party origin the operator allowlisted. Deliberately after
	// both distribution checks, so an archive apt-proxy mirrors keeps its own
	// routing and cache rules.
	return ap.matchPassthrough(r)
}

// requestHost returns the host the client addressed, lower-cased. net/http
// moves the Host header into r.Host and leaves r.URL.Host empty for server
// requests, but a proxied absolute-form request populates r.URL.Host, so
// prefer that. DNS names are case-insensitive, so the result is normalised and
// host patterns are written in lower case.
func requestHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	if r.URL != nil && r.URL.Host != "" {
		return strings.ToLower(r.URL.Host)
	}
	return strings.ToLower(r.Host)
}

// processMatchingRule processes a request that matches a distribution pattern.
// It finds the specific caching rule, removes client cache control headers,
// and rewrites the URL if necessary.
func (ap *PackageStruct) processMatchingRule(r *http.Request, rt *routing, rules []distro.Rule) *distro.Rule {
	rule, match := MatchingRule(r.URL.Path, rules)
	if !match {
		return nil
	}

	r.Header.Del("Cache-Control")
	if rule.Rewrite {
		ap.rewriteRequest(r, rt, rule)
	}
	return rule
}

// rewriteRequest rewrites the request URL to point to the configured mirror
// for the distribution. This enables transparent proxying to different mirrors
// while maintaining the original request path structure.
func (ap *PackageStruct) rewriteRequest(r *http.Request, rt *routing, rule *distro.Rule) {
	if r.URL == nil {
		ap.log.Error().Msg("request URL is nil, cannot rewrite")
		return
	}
	before := r.URL.String()
	RewriteRequestByMode(r, rt.rewriters, rule.OS)

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
	if ap == nil {
		return
	}
	ap.refreshMu.Lock()
	defer ap.refreshMu.Unlock()

	// Build both halves against the reloaded registry before either is
	// visible. buildRewriters is the slow part -- it re-elects mirrors --
	// and for its whole duration requests keep serving the previous
	// snapshot, matching and rewriting consistently with each other.
	next := &routing{
		patterns:  hostPatternsFromRegistry(ap.registry),
		rewriters: buildRewriters(ap.mode, ap.state, ap.registry, ap.bench),
	}
	ap.routing.Store(next)
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
	if status == http.StatusNotFound && rw.rule != nil {
		// Mirror publication is not atomic: a package or index can briefly be
		// absent while the mirror synchronizes. Keep negative caching short and
		// independent from immutable package TTLs.
		rw.Header().Set("Cache-Control", "public, max-age=30")
	} else if rw.shouldSetCacheControl(status) {
		rw.Header().Set("Cache-Control", rw.rule.CacheControl)
	}
	rw.ResponseWriter.WriteHeader(status)
}

// shouldSetCacheControl determines whether cache control headers should be set
// for the given HTTP status code. Only certain status codes are cacheable.
func (rw *responseWriter) shouldSetCacheControl(status int) bool {
	return rw.rule != nil &&
		rw.rule.CacheControl != "" &&
		status == http.StatusOK
}
