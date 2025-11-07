package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"time"

	define "github.com/soulteary/apt-proxy/define"
	rewriter "github.com/soulteary/apt-proxy/internal/rewriter"
	state "github.com/soulteary/apt-proxy/state"
)

var hostPatternMap = map[*regexp.Regexp][]define.Rule{
	define.UBUNTU_HOST_PATTERN:       define.UBUNTU_DEFAULT_CACHE_RULES,
	define.UBUNTU_PORTS_HOST_PATTERN: define.UBUNTU_PORTS_DEFAULT_CACHE_RULES,
	define.DEBIAN_HOST_PATTERN:       define.DEBIAN_DEFAULT_CACHE_RULES,
	define.CENTOS_HOST_PATTERN:       define.CENTOS_DEFAULT_CACHE_RULES,
	define.ALPINE_HOST_PATTERN:       define.ALPINE_DEFAULT_CACHE_RULES,
}

var (
	rewriters        *rewriter.URLRewriters
	defaultTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 45 * time.Second,
		DisableKeepAlives:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    false,
	}
)

// PackageStruct is the main HTTP handler that routes requests to appropriate
// distribution-specific handlers and applies caching rules.
type PackageStruct struct {
	Handler  http.Handler  // The underlying HTTP handler (typically a reverse proxy)
	Rules    []define.Rule // Caching rules for different package types
	CacheDir string        // Cache directory path for statistics
}

// responseWriter wraps http.ResponseWriter to inject cache control headers
// based on the matched caching rule.
type responseWriter struct {
	http.ResponseWriter
	rule *define.Rule // The matched caching rule for this request
}

// CreatePackageStructRouter initializes and returns a new PackageStruct instance
// configured for the current proxy mode. It sets up URL rewriters and
// caching rules based on the configured distribution mode.
func CreatePackageStructRouter(cacheDir string) *PackageStruct {
	mode := state.GetProxyMode()
	rewriters = rewriter.CreateNewRewriters(mode)

	return &PackageStruct{
		Rules:    rewriter.GetRewriteRulesByMode(mode),
		CacheDir: cacheDir,
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: defaultTransport,
		},
	}
}

// ServeHTTP implements http.Handler interface. It processes incoming requests,
// matches them against caching rules, and routes them to the appropriate handler.
// If a matching rule is found, the request is processed with cache control headers.
func (ap *PackageStruct) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if rule := ap.handleRequest(rw, r); rule != nil {
		if ap.Handler != nil {
			ap.Handler.ServeHTTP(&responseWriter{rw, rule}, r)
		} else {
			http.Error(rw, "Internal Server Error: handler not initialized", http.StatusInternalServerError)
		}
	}
}

// handleRequest processes the incoming request and determines which handler
// should process it. Returns a matching caching rule if found, nil otherwise.
func (ap *PackageStruct) handleRequest(rw http.ResponseWriter, r *http.Request) *define.Rule {
	if IsInternalUrls(r.URL.Path) {
		return ap.handleInternalURLs(rw, r)
	}
	return ap.handleExternalURLs(r)
}

// handleInternalURLs processes requests for internal pages (e.g., status page, ping endpoint).
// These requests are served directly without proxying or caching.
func (ap *PackageStruct) handleInternalURLs(rw http.ResponseWriter, r *http.Request) *define.Rule {
	tpl, status := RenderInternalUrls(r.URL.Path, ap.CacheDir)
	rw.WriteHeader(status)
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(rw, tpl); err != nil {
		log.Printf("Error rendering internal URLs: %v", err)
	}
	return nil
}

// handleExternalURLs processes requests for external package repositories.
// It matches the request path against known distribution patterns and returns
// the appropriate caching rule if a match is found.
func (ap *PackageStruct) handleExternalURLs(r *http.Request) *define.Rule {
	path := r.URL.Path
	for pattern, rules := range hostPatternMap {
		if pattern.MatchString(path) {
			return ap.processMatchingRule(r, rules)
		}
	}
	return nil
}

// processMatchingRule processes a request that matches a distribution pattern.
// It finds the specific caching rule, removes client cache control headers,
// and rewrites the URL if necessary.
func (ap *PackageStruct) processMatchingRule(r *http.Request, rules []define.Rule) *define.Rule {
	rule, match := rewriter.MatchingRule(r.URL.Path, rules)
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
func (ap *PackageStruct) rewriteRequest(r *http.Request, rule *define.Rule) {
	if r.URL == nil {
		log.Printf("Error: request URL is nil, cannot rewrite")
		return
	}
	before := r.URL.String()
	rewriter.RewriteRequestByMode(r, rewriters, rule.OS)

	if r.URL != nil {
		r.Host = r.URL.Host
		log.Printf("Rewrote %q to %q", before, r.URL.String())
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
