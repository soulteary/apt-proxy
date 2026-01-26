package rewriter

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	logger "github.com/soulteary/logger-kit"

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
	Ubuntu      *URLRewriter
	UbuntuPorts *URLRewriter
	Debian      *URLRewriter
	Centos      *URLRewriter
	Alpine      *URLRewriter
	Mu          sync.RWMutex
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
	log := logger.Default()
	getMirror, name := getRewriterConfig(mode)
	if getMirror == nil {
		return nil
	}

	benchmarkURL, pattern := mirrors.GetPredefinedConfiguration(mode)
	rewriter := &URLRewriter{pattern: pattern}
	mirror := getMirror()

	if mirror != nil {
		log.Info().Str("distro", name).Str("mirror", mirror.String()).Msg("using specified mirror")
		rewriter.mirror = mirror
		return rewriter
	}

	mirrorURLs := mirrors.GetGeoMirrorUrlsByMode(mode)
	fastest, err := benchmarks.GetTheFastestMirror(mirrorURLs, benchmarkURL)
	if err != nil {
		log.Error().Err(err).Str("distro", name).Msg("error finding fastest mirror")
		return rewriter
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Info().Str("distro", name).Str("mirror", fastest).Msg("using fastest mirror")
		rewriter.mirror = mirror
	}

	return rewriter
}

// CreateNewRewriters initializes rewriters based on mode
func CreateNewRewriters(mode int) *URLRewriters {
	rewriters := &URLRewriters{}

	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriters.Ubuntu = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriters.UbuntuPorts = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriters.Debian = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriters.Centos = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriters.Alpine = createRewriter(mode)
	default:
		rewriters.Ubuntu = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU)
		rewriters.UbuntuPorts = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
		rewriters.Debian = createRewriter(define.TYPE_LINUX_DISTROS_DEBIAN)
		rewriters.Centos = createRewriter(define.TYPE_LINUX_DISTROS_CENTOS)
		rewriters.Alpine = createRewriter(define.TYPE_LINUX_DISTROS_ALPINE)
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

// RewriteRequestByMode rewrites the request URL to point to the configured mirror
// for the specified distribution mode. It matches the request path against
// distribution-specific patterns and replaces the URL scheme, host, and path
// with the mirror's configuration. If rewriters is nil, the function returns early.
func RewriteRequestByMode(r *http.Request, rewriters *URLRewriters, mode int) {
	if rewriters == nil {
		return
	}
	rewriters.Mu.RLock()
	defer rewriters.Mu.RUnlock()

	rewriter := &URLRewriter{}
	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriter = rewriters.Ubuntu
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriter = rewriters.UbuntuPorts
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriter = rewriters.Debian
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriter = rewriters.Centos
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriter = rewriters.Alpine
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
	if mode == define.TYPE_LINUX_DISTROS_DEBIAN {
		slugs_query := strings.Split(r.URL.Path, "/")
		slugs_mirror := strings.Split(rewriter.mirror.Path, "/")
		slugs_mirror[0] = slugs_query[0]
		r.URL.Path = strings.Join(slugs_query, "/")
		return
	}
	// Use templates for path construction
	path, err := mirrors.BuildPathWithQuery(rewriter.mirror.Path, unescapedQuery)
	if err != nil {
		// Fallback to concatenation if template fails
		r.URL.Path = rewriter.mirror.Path + unescapedQuery
	} else {
		r.URL.Path = path
	}
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

// RefreshRewriters refreshes the rewriters with updated mirror configurations.
// This function is safe to call concurrently and will update the mirrors
// based on the current state configuration.
func RefreshRewriters(rewriters *URLRewriters, mode int) {
	if rewriters == nil {
		return
	}

	log := logger.Default()
	log.Info().Msg("refreshing mirror configurations...")

	rewriters.Mu.Lock()
	defer rewriters.Mu.Unlock()

	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriters.Ubuntu = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriters.UbuntuPorts = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriters.Debian = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriters.Centos = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriters.Alpine = createRewriter(mode)
	default:
		rewriters.Ubuntu = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU)
		rewriters.UbuntuPorts = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
		rewriters.Debian = createRewriter(define.TYPE_LINUX_DISTROS_DEBIAN)
		rewriters.Centos = createRewriter(define.TYPE_LINUX_DISTROS_CENTOS)
		rewriters.Alpine = createRewriter(define.TYPE_LINUX_DISTROS_ALPINE)
	}

	log.Info().Msg("mirror configurations refreshed successfully")
}
