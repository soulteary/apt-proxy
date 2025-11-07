package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"time"

	Define "github.com/soulteary/apt-proxy/define"
	Rewriter "github.com/soulteary/apt-proxy/internal/rewriter"
	State "github.com/soulteary/apt-proxy/state"
)

var hostPatternMap = map[*regexp.Regexp][]Define.Rule{
	Define.UBUNTU_HOST_PATTERN:       Define.UBUNTU_DEFAULT_CACHE_RULES,
	Define.UBUNTU_PORTS_HOST_PATTERN: Define.UBUNTU_PORTS_DEFAULT_CACHE_RULES,
	Define.DEBIAN_HOST_PATTERN:       Define.DEBIAN_DEFAULT_CACHE_RULES,
	Define.CENTOS_HOST_PATTERN:       Define.CENTOS_DEFAULT_CACHE_RULES,
	Define.ALPINE_HOST_PATTERN:       Define.ALPINE_DEFAULT_CACHE_RULES,
}

var (
	rewriters        *Rewriter.URLRewriters
	defaultTransport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 45 * time.Second,
		DisableKeepAlives:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    false,
	}
)

// AptProxy is the main HTTP handler that routes requests to appropriate
// distribution-specific handlers and applies caching rules.
type AptProxy struct {
	Handler http.Handler // The underlying HTTP handler (typically a reverse proxy)
	Rules   []Define.Rule // Caching rules for different package types
}

// responseWriter wraps http.ResponseWriter to inject cache control headers
// based on the matched caching rule.
type responseWriter struct {
	http.ResponseWriter
	rule *Define.Rule // The matched caching rule for this request
}

// CreateAptProxyRouter initializes and returns a new AptProxy instance
// configured for the current proxy mode. It sets up URL rewriters and
// caching rules based on the configured distribution mode.
func CreateAptProxyRouter() *AptProxy {
	mode := State.GetProxyMode()
	rewriters = Rewriter.CreateNewRewriters(mode)

	return &AptProxy{
		Rules: Rewriter.GetRewriteRulesByMode(mode),
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: defaultTransport,
		},
	}
}

// ServeHTTP implements http.Handler interface. It processes incoming requests,
// matches them against caching rules, and routes them to the appropriate handler.
// If a matching rule is found, the request is processed with cache control headers.
func (ap *AptProxy) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
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
func (ap *AptProxy) handleRequest(rw http.ResponseWriter, r *http.Request) *Define.Rule {
	if IsInternalUrls(r.URL.Path) {
		return ap.handleInternalURLs(rw, r)
	}
	return ap.handleExternalURLs(r)
}

// handleInternalURLs processes requests for internal pages (e.g., status page, ping endpoint).
// These requests are served directly without proxying or caching.
func (ap *AptProxy) handleInternalURLs(rw http.ResponseWriter, r *http.Request) *Define.Rule {
	tpl, status := RenderInternalUrls(r.URL.Path)
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
func (ap *AptProxy) handleExternalURLs(r *http.Request) *Define.Rule {
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
func (ap *AptProxy) processMatchingRule(r *http.Request, rules []Define.Rule) *Define.Rule {
	rule, match := Rewriter.MatchingRule(r.URL.Path, rules)
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
func (ap *AptProxy) rewriteRequest(r *http.Request, rule *Define.Rule) {
	if r.URL == nil {
		log.Printf("Error: request URL is nil, cannot rewrite")
		return
	}
	before := r.URL.String()
	Rewriter.RewriteRequestByMode(r, rewriters, rule.OS)

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
