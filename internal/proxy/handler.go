package proxy

import (
	"io"
	"net/http"
	"net/http/httputil"
	"regexp"
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

// getHostPatternMap returns pattern -> rules from registry (config-loaded or built-in)
func getHostPatternMap() map[*regexp.Regexp][]distro.Rule {
	m := distro.GetHostPatternMap()
	if len(m) > 0 {
		return m
	}
	return defaultHostPatternMap
}

var defaultHostPatternMap = map[*regexp.Regexp][]distro.Rule{
	distro.UBUNTU_HOST_PATTERN:       distro.UBUNTU_DEFAULT_CACHE_RULES,
	distro.UBUNTU_PORTS_HOST_PATTERN: distro.UBUNTU_PORTS_DEFAULT_CACHE_RULES,
	distro.DEBIAN_HOST_PATTERN:       distro.DEBIAN_DEFAULT_CACHE_RULES,
	distro.CENTOS_HOST_PATTERN:       distro.CENTOS_DEFAULT_CACHE_RULES,
	distro.ALPINE_HOST_PATTERN:       distro.ALPINE_DEFAULT_CACHE_RULES,
}

var (
	rewriters        *URLRewriters
	defaultTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
		DisableKeepAlives:     true,
		MaxIdleConns:          DefaultMaxIdleConns,
		IdleConnTimeout:       DefaultIdleConnTimeout,
		DisableCompression:    false,
	}
	// retryableTransport wraps defaultTransport with retry logic and tracing
	retryableTransport = NewRetryableTransport(defaultTransport)
)

// PackageStruct is the main HTTP handler that routes requests to appropriate
// distribution-specific handlers and applies caching rules.
type PackageStruct struct {
	Handler  http.Handler   // The underlying HTTP handler (typically a reverse proxy)
	Rules    []distro.Rule  // Caching rules for different package types
	CacheDir string         // Cache directory path for statistics
	log      *logger.Logger // Structured logger
}

// responseWriter wraps http.ResponseWriter to inject cache control headers
// based on the matched caching rule.
type responseWriter struct {
	http.ResponseWriter
	rule *distro.Rule // The matched caching rule for this request
}

// CreatePackageStructRouter initializes and returns a new PackageStruct instance
// configured for the current proxy mode. It sets up URL rewriters and caching rules.
// Uses synchronous benchmark which may block startup. For faster startup, use
// CreatePackageStructRouterAsync instead.
func CreatePackageStructRouter(cacheDir string, log *logger.Logger) *PackageStruct {
	mode := state.GetProxyMode()
	rewriters = CreateNewRewriters(mode)

	return &PackageStruct{
		Rules:    GetRewriteRulesByMode(mode),
		CacheDir: cacheDir,
		log:      log,
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: retryableTransport,
		},
	}
}

// CreatePackageStructRouterAsync initializes and returns a new PackageStruct instance
// configured for the current proxy mode using async benchmark.
// This allows faster startup by using default mirrors immediately while
// benchmarking runs in the background to find the fastest mirror.
func CreatePackageStructRouterAsync(cacheDir string, log *logger.Logger) *PackageStruct {
	mode := state.GetProxyMode()
	rewriters = CreateNewRewritersAsync(mode)

	return &PackageStruct{
		Rules:    GetRewriteRulesByMode(mode),
		CacheDir: cacheDir,
		log:      log,
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: retryableTransport,
		},
	}
}

// HandleHomePage serves the home page with statistics
func HandleHomePage(rw http.ResponseWriter, r *http.Request, cacheDir string) {
	tpl, status := RenderInternalUrls("/", cacheDir)
	rw.WriteHeader(status)
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
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
		// Set distribution rule attribute
		distroName := getDistributionName(rule.OS)
		if distroName != "" {
			tracing.SetSpanAttributes(span, map[string]string{
				"proxy.distribution": distroName,
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
	for pattern, rules := range getHostPatternMap() {
		if pattern.MatchString(path) {
			return ap.processMatchingRule(r, rules)
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
	RewriteRequestByMode(r, rewriters, rule.OS)

	if r.URL != nil {
		r.Host = r.URL.Host
		ap.log.Debug().
			Str("from", before).
			Str("to", r.URL.String()).
			Msg("rewrote request URL")
	}
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
// This is typically called in response to a SIGHUP signal for hot reload.
func RefreshMirrors() {
	mode := state.GetProxyMode()
	RefreshRewriters(rewriters, mode)
}

// getDistributionName converts distribution type int to string name
func getDistributionName(distType int) string {
	switch distType {
	case distro.TYPE_LINUX_DISTROS_UBUNTU:
		return distro.LINUX_DISTROS_UBUNTU
	case distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return distro.LINUX_DISTROS_UBUNTU_PORTS
	case distro.TYPE_LINUX_DISTROS_DEBIAN:
		return distro.LINUX_DISTROS_DEBIAN
	case distro.TYPE_LINUX_DISTROS_CENTOS:
		return distro.LINUX_DISTROS_CENTOS
	case distro.TYPE_LINUX_DISTROS_ALPINE:
		return distro.LINUX_DISTROS_ALPINE
	default:
		return ""
	}
}
