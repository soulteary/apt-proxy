package proxy

import (
	"io"
	"net/http"
	"net/http/httputil"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	logger "github.com/soulteary/logger-kit"
	tracing "github.com/soulteary/tracing-kit"

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

// hostPatternCache caches the snapshot from distro.GetHostPatternMap so we
// don't allocate/copy a fresh map for every request. RefreshMirrors clears
// this pointer on hot reload; readers fall back to defaultHostPatterns when
// the registry returns an empty result.
var hostPatternCache atomic.Pointer[[]hostPatternEntry]

// hostPatternsFromRegistry materialises the registry's pattern→rules map
// into a stable, ordered slice. Distros are walked via distroModesOrder so
// matching is deterministic for callers that have multiple overlapping rules.
func hostPatternsFromRegistry() []hostPatternEntry {
	reg := distro.GetRegistry()
	if reg == nil {
		return nil
	}
	all := reg.GetAll()
	out := make([]hostPatternEntry, 0, len(all))
	// Walk known distros in a stable order first.
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
	// Then any additional registered distros (config-loaded with an unknown type).
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

// getHostPatterns returns the cached pattern→rules entries, populating the
// cache on first access. The fallback (defaultHostPatterns) is used only if
// the registry has nothing to offer (e.g. during a unit test that nuked it).
func getHostPatterns() []hostPatternEntry {
	if cur := hostPatternCache.Load(); cur != nil && len(*cur) > 0 {
		return *cur
	}
	entries := hostPatternsFromRegistry()
	if len(entries) == 0 {
		return defaultHostPatterns
	}
	hostPatternCache.Store(&entries)
	return entries
}

// invalidateHostPatternCache forces the next request to rebuild the cache
// from the registry. Called from RefreshMirrors / SIGHUP reload.
func invalidateHostPatternCache() {
	hostPatternCache.Store(nil)
}

// defaultHostPatterns is the compile-time fallback used when no registry
// entries are available. The order matches distroModesOrder.
var defaultHostPatterns = []hostPatternEntry{
	{pattern: distro.UbuntuHostPattern, rules: distro.UbuntuDefaultCacheRules},
	{pattern: distro.UbuntuPortsHostPattern, rules: distro.UbuntuPortsDefaultCacheRules},
	{pattern: distro.DebianHostPattern, rules: distro.DebianDefaultCacheRules},
	{pattern: distro.CentosHostPattern, rules: distro.CentosDefaultCacheRules},
	{pattern: distro.AlpineHostPattern, rules: distro.AlpineDefaultCacheRules},
}

var (
	rewriters        *URLRewriters
	defaultTransport *http.Transport
	// retryableTransport wraps defaultTransport with retry logic and tracing
	retryableTransport *RetryableTransport
	// rewritersMu serializes the package-level rewriters pointer swap.
	// RewriteRequestByMode reads the value of `rewriters` while holding it as
	// a parameter, so this lock only protects writers (init / refresh) racing
	// each other (e.g. SIGHUP + /api/mirrors/refresh).
	rewritersMu sync.Mutex
)

func init() {
	// Default: enable HTTP keep-alive to upstream mirrors. Reusing TCP/TLS
	// connections to a small set of mirrors is dramatically cheaper than
	// dialling per request, especially for HTTPS mirrors where every request
	// would otherwise pay a fresh handshake.
	initUpstreamTransport(false)
}

// initUpstreamTransport sets the package-level upstream transport.
// When disableKeepAlives is true, connections are not reused.
// When false (default), HTTP keep-alive to upstream mirrors is enabled.
func initUpstreamTransport(disableKeepAlives bool) {
	defaultTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
		DisableKeepAlives:     disableKeepAlives,
		MaxIdleConns:          DefaultMaxIdleConns,
		IdleConnTimeout:       DefaultIdleConnTimeout,
		DisableCompression:    false,
	}
	retryableTransport = NewRetryableTransport(defaultTransport)
}

// InitUpstreamTransport configures the upstream HTTP transport (e.g. from config).
// Call before CreatePackageStructRouter or CreatePackageStructRouterAsync.
// enableKeepAlive: true = reuse connections to mirrors (recommended); false = disable keep-alives.
func InitUpstreamTransport(enableKeepAlive bool) {
	initUpstreamTransport(!enableKeepAlive)
}

// PackageStruct is the main HTTP handler that routes requests to appropriate
// distribution-specific handlers and applies caching rules.
type PackageStruct struct {
	Handler  http.Handler   // The underlying HTTP handler (typically a reverse proxy)
	Rules    []distro.Rule  // Caching rules for different package types
	CacheDir string         // Cache directory path for statistics
	log      *logger.Logger // Structured logger
	// rewriters is the per-instance URL rewriter set. It mirrors the package
	// global so existing tests/utilities keep working, but ServeHTTP prefers
	// the instance field; concurrent Servers in the same process can hold
	// independent rewriters this way.
	rewriters *URLRewriters
	mode      int
}

// responseWriter wraps http.ResponseWriter to inject cache control headers
// based on the matched caching rule.
type responseWriter struct {
	http.ResponseWriter
	rule *distro.Rule // The matched caching rule for this request
}

// createPackageStruct initializes a PackageStruct with the given rewriter factory.
func createPackageStruct(cacheDir string, log *logger.Logger, rewriterFactory func(int) *URLRewriters) *PackageStruct {
	mode := state.GetProxyMode()
	rewritersMu.Lock()
	rewriters = rewriterFactory(mode)
	current := rewriters
	rewritersMu.Unlock()
	return &PackageStruct{
		Rules:     GetRewriteRulesByMode(mode),
		CacheDir:  cacheDir,
		log:       log,
		rewriters: current,
		mode:      mode,
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: retryableTransport,
		},
	}
}

// CreatePackageStructRouter initializes and returns a new PackageStruct instance
// configured for the current proxy mode. Uses synchronous benchmark (may block startup).
// For faster startup, use CreatePackageStructRouterAsync instead.
func CreatePackageStructRouter(cacheDir string, log *logger.Logger) *PackageStruct {
	return createPackageStruct(cacheDir, log, CreateNewRewriters)
}

// CreatePackageStructRouterAsync initializes and returns a new PackageStruct instance
// using async benchmark for faster startup (recommended).
func CreatePackageStructRouterAsync(cacheDir string, log *logger.Logger) *PackageStruct {
	return createPackageStruct(cacheDir, log, CreateNewRewritersAsync)
}

// HandleHomePage serves the home page with statistics
func HandleHomePage(rw http.ResponseWriter, r *http.Request, cacheDir string) {
	tpl, status := RenderInternalUrls("/", cacheDir)
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	if _, err := io.WriteString(rw, tpl); err != nil {
		logger.Error().Err(err).Msg("Error rendering home page")
	}
}

// ServeHTTP implements http.Handler interface. It processes incoming requests,
// matches them against caching rules, and routes them to the appropriate handler.
// If a matching rule is found, the request is processed with cache control headers.
func (ap *PackageStruct) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Start tracing span for the request
	spanCtx, span := tracing.StartSpan(ctx, "proxy.request")
	defer span.End()

	// Set span attributes
	tracing.SetSpanAttributesFromMap(span, map[string]interface{}{
		"http.method":      r.Method,
		"http.url":         r.URL.String(),
		"http.path":        r.URL.Path,
		"http.scheme":      r.URL.Scheme,
		"http.host":        r.Host,
		"http.user_agent":  r.UserAgent(),
		"http.remote_addr": r.RemoteAddr,
	})

	// Update request context with span context
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

// handleExternalURLs processes requests for external package repositories.
// It matches the request path against known distribution patterns and returns
// the appropriate caching rule if a match is found.
func (ap *PackageStruct) handleExternalURLs(r *http.Request) *distro.Rule {
	path := r.URL.Path
	for _, entry := range getHostPatterns() {
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
	rw := ap.rewriters
	if rw == nil {
		rw = rewriters
	}
	RewriteRequestByMode(r, rw, rule.OS)

	if r.URL != nil {
		r.Host = r.URL.Host
		ap.log.Debug().
			Str("from", before).
			Str("to", r.URL.String()).
			Msg("rewrote request URL")
	}
}

// RefreshMirrors refreshes this PackageStruct's mirror configuration.
// Use this method on a PackageStruct instance instead of the package-level
// proxy.RefreshMirrors() to avoid touching the global state shared with
// other instances or tests.
func (ap *PackageStruct) RefreshMirrors() {
	if ap == nil || ap.rewriters == nil {
		return
	}
	invalidateHostPatternCache()
	RefreshRewriters(ap.rewriters, ap.mode)
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

// RefreshMirrors refreshes the mirror configurations for all distributions.
// This is typically called in response to a SIGHUP signal for hot reload,
// and from the /api/mirrors/refresh endpoint. The mutex serializes concurrent
// refreshes (the rewriter pointer swap inside RefreshRewriters has its own
// finer-grained lock; this outer lock prevents two refresh runs from racing
// to clear the benchmark cache and re-elect mirrors at the same time).
func RefreshMirrors() {
	mode := state.GetProxyMode()
	rewritersMu.Lock()
	current := rewriters
	rewritersMu.Unlock()
	invalidateHostPatternCache()
	RefreshRewriters(current, mode)
}
