package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"time"

	Define "github.com/apham0001/apt-proxy/define"
	Rewriter "github.com/apham0001/apt-proxy/internal/rewriter"
	State "github.com/apham0001/apt-proxy/state"
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

type AptProxy struct {
	Handler http.Handler
	Rules   []Define.Rule
}

type responseWriter struct {
	http.ResponseWriter
	rule *Define.Rule
}

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

func (ap *AptProxy) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if rule := ap.handleRequest(rw, r); rule != nil {
		ap.Handler.ServeHTTP(&responseWriter{rw, rule}, r)
	}
}

func (ap *AptProxy) handleRequest(rw http.ResponseWriter, r *http.Request) *Define.Rule {
	if IsInternalUrls(r.URL.Path) {
		return ap.handleInternalURLs(rw, r)
	}
	return ap.handleExternalURLs(r)
}

func (ap *AptProxy) handleInternalURLs(rw http.ResponseWriter, r *http.Request) *Define.Rule {
	tpl, status := RenderInternalUrls(r.URL.Path)
	rw.WriteHeader(status)
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(rw, tpl); err != nil {
		log.Printf("Error rendering internal URLs: %v", err)
	}
	return nil
}

func (ap *AptProxy) handleExternalURLs(r *http.Request) *Define.Rule {
	path := r.URL.Path
	for pattern, rules := range hostPatternMap {
		if pattern.MatchString(path) {
			return ap.processMatchingRule(r, rules)
		}
	}
	return nil
}

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

func (ap *AptProxy) rewriteRequest(r *http.Request, rule *Define.Rule) {
	before := r.URL.String()
	Rewriter.RewriteRequestByMode(r, rewriters, rule.OS)

	r.Host = r.URL.Host
	log.Printf("Rewrote %q to %q", before, r.URL.String())
}

func (rw *responseWriter) WriteHeader(status int) {
	if rw.shouldSetCacheControl(status) {
		rw.Header().Set("Cache-Control", rw.rule.CacheControl)
	}
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) shouldSetCacheControl(status int) bool {
	return rw.rule != nil &&
		rw.rule.CacheControl != "" &&
		(status == http.StatusOK || status == http.StatusNotFound)
}
