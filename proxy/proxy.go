package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/soulteary/apt-proxy/linux"
)

var rewriter *linux.URLRewriter

var defaultTransport http.RoundTripper = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: time.Second * 45,
	DisableKeepAlives:     true,
}

type AptProxy struct {
	Handler http.Handler
	Rules   []Rule
}

func NewAptProxyFromDefaults(mirror string, osType string) *AptProxy {
	rewriter = linux.NewRewriter(mirror, osType)
	return &AptProxy{
		Rules: DefaultRules,
		Handler: &httputil.ReverseProxy{
			Director:  func(r *http.Request) {},
			Transport: defaultTransport,
		},
	}
}

func (ap *AptProxy) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rule, match := matchingRule(r.URL.Path, ap.Rules)
	if match {
		r.Header.Del("Cache-Control")
		if rule.Rewrite {
			ap.rewriteRequest(r)
		}
	}

	ap.Handler.ServeHTTP(&responseWriter{rw, rule}, r)
}

func (ap *AptProxy) rewriteRequest(r *http.Request) {
	before := r.URL.String()
	linux.Rewrite(r, rewriter)
	log.Printf("rewrote %q to %q", before, r.URL.String())
	r.Host = r.URL.Host
}

type responseWriter struct {
	http.ResponseWriter
	rule *Rule
}

func (rw *responseWriter) WriteHeader(status int) {
	if rw.rule != nil && rw.rule.CacheControl != "" &&
		(status == http.StatusOK || status == http.StatusNotFound) {
		rw.Header().Set("Cache-Control", rw.rule.CacheControl)
	}
	rw.ResponseWriter.WriteHeader(status)
}
