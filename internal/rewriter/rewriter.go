package rewriter

import (
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sync"

	"github.com/soulteary/apt-proxy/define"
	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/state"
)

// URLRewriter holds the mirror and pattern for URL rewriting
type URLRewriter struct {
	mirror  *url.URL
	pattern *regexp.Regexp
}

// URLRewriters manages rewriters for different distributions
type URLRewriters struct {
	ubuntu      *URLRewriter
	ubuntuPorts *URLRewriter
	debian      *URLRewriter
	centos      *URLRewriter
	alpine      *URLRewriter
	mu          sync.RWMutex
}

// getRewriterConfig returns configuration for a specific distribution
func getRewriterConfig(mode int) (getMirror func() *url.URL, name string) {
	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		return state.GetUbuntuMirror, "Ubuntu"
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return state.GetUbuntuPortsMirror, "Ubuntu Ports"
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		return state.GetDebianMirror, "Debian"
	case define.TYPE_LINUX_DISTROS_CENTOS:
		return state.GetCentOSMirror, "CentOS"
	case define.TYPE_LINUX_DISTROS_ALPINE:
		return state.GetAlpineMirror, "Alpine"
	default:
		return nil, ""
	}
}

// createRewriter creates a new URLRewriter for a specific distribution
func createRewriter(mode int) *URLRewriter {
	getMirror, name := getRewriterConfig(mode)
	if getMirror == nil {
		return nil
	}

	benchmarkURL, pattern := mirrors.GetPredefinedConfiguration(mode)
	rewriter := &URLRewriter{pattern: pattern}
	mirror := getMirror()

	if mirror != nil {
		log.Printf("using specified %s mirror %s", name, mirror)
		rewriter.mirror = mirror
		return rewriter
	}

	mirrorURLs := mirrors.GetGeoMirrorUrlsByMode(mode)
	fastest, err := benchmarks.GetTheFastestMirror(mirrorURLs, benchmarkURL)
	if err != nil {
		log.Printf("Error finding fastest %s mirror: %v", name, err)
		return rewriter
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Printf("using fastest %s mirror %s", name, fastest)
		rewriter.mirror = mirror
	}

	return rewriter
}

// CreateNewRewriters initializes rewriters based on mode
func CreateNewRewriters(mode int) *URLRewriters {
	rewriters := &URLRewriters{}

	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriters.ubuntu = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriters.ubuntuPorts = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriters.debian = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriters.centos = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriters.alpine = createRewriter(mode)
	default:
		rewriters.ubuntu = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU)
		rewriters.ubuntuPorts = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
		rewriters.debian = createRewriter(define.TYPE_LINUX_DISTROS_DEBIAN)
		rewriters.centos = createRewriter(define.TYPE_LINUX_DISTROS_CENTOS)
		rewriters.alpine = createRewriter(define.TYPE_LINUX_DISTROS_ALPINE)
	}

	return rewriters
}

// GetRewriteRulesByMode returns caching rules for a specific mode
func GetRewriteRulesByMode(mode int) []define.Rule {
	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		return define.UBUNTU_DEFAULT_CACHE_RULES
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return define.UBUNTU_PORTS_DEFAULT_CACHE_RULES
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		return define.DEBIAN_DEFAULT_CACHE_RULES
	case define.TYPE_LINUX_DISTROS_CENTOS:
		return define.CENTOS_DEFAULT_CACHE_RULES
	case define.TYPE_LINUX_DISTROS_ALPINE:
		return define.ALPINE_DEFAULT_CACHE_RULES
	default:
		rules := make([]define.Rule, 0)
		rules = append(rules, define.UBUNTU_DEFAULT_CACHE_RULES...)
		rules = append(rules, define.UBUNTU_PORTS_DEFAULT_CACHE_RULES...)
		rules = append(rules, define.DEBIAN_DEFAULT_CACHE_RULES...)
		rules = append(rules, define.CENTOS_DEFAULT_CACHE_RULES...)
		rules = append(rules, define.ALPINE_DEFAULT_CACHE_RULES...)
		return rules
	}
}

// RewriteRequestByMode rewrites the request URL based on the mode
func RewriteRequestByMode(r *http.Request, rewriters *URLRewriters, mode int) {
	rewriters.mu.RLock()
	defer rewriters.mu.RUnlock()

	var rewriter *URLRewriter
	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriter = rewriters.ubuntu
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriter = rewriters.ubuntuPorts
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriter = rewriters.debian
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriter = rewriters.centos
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriter = rewriters.alpine
	}

	if rewriter == nil || rewriter.mirror == nil || rewriter.pattern == nil {
		return
	}

	uri := r.URL.String()
	if !rewriter.pattern.MatchString(uri) {
		return
	}

	r.Header.Add("Content-Location", uri)
	matches := rewriter.pattern.FindStringSubmatch(uri)
	if len(matches) == 0 {
		return
	}

	queryRaw := matches[len(matches)-1]
	unescapedQuery, err := url.PathUnescape(queryRaw)
	if err != nil {
		unescapedQuery = queryRaw
	}

	r.URL.Scheme = rewriter.mirror.Scheme
	r.URL.Host = rewriter.mirror.Host
	r.URL.Path = rewriter.mirror.Path + unescapedQuery
}

// MatchingRule finds a matching rule for the given path
func MatchingRule(path string, rules []define.Rule) (*define.Rule, bool) {
	for _, rule := range rules {
		if rule.Pattern.MatchString(path) {
			return &rule, true
		}
	}
	return nil, false
}
