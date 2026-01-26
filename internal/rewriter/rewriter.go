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

// createRewriter creates a new URLRewriter for a specific distribution.
// It uses the cached benchmark result if available, otherwise runs a synchronous benchmark.
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
	// Use cache-aware benchmark to avoid repeated testing
	fastest, err := benchmarks.GetTheFastestMirrorWithCache(mode, mirrorURLs, benchmarkURL)
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

// createRewriterAsync creates a new URLRewriter for a specific distribution using async benchmark.
// It immediately returns with a default mirror and updates the mirror in the background.
func createRewriterAsync(mode int, rewriters *URLRewriters) *URLRewriter {
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

	// Check if we have a cached result
	if cached, ok := benchmarks.GetBenchmarkCache().GetCachedResult(mode); ok {
		if parsedMirror, err := url.Parse(cached); err == nil {
			log.Info().Str("distro", name).Str("mirror", cached).Msg("using cached mirror")
			rewriter.mirror = parsedMirror
			return rewriter
		}
	}

	// Use default mirror immediately for fast startup
	defaultMirror := benchmarks.GetDefaultMirror(mirrorURLs)
	if parsedMirror, err := url.Parse(defaultMirror); err == nil {
		log.Info().Str("distro", name).Str("mirror", defaultMirror).Msg("using default mirror (async benchmark pending)")
		rewriter.mirror = parsedMirror
	}

	// Run benchmark in background and update when complete
	benchmarks.GetTheFastestMirrorAsync(mode, mirrorURLs, benchmarkURL, func(result benchmarks.AsyncBenchmarkResult) {
		if result.Error != nil {
			log.Error().Err(result.Error).Str("distro", name).Msg("async benchmark failed")
			return
		}

		parsedMirror, err := url.Parse(result.FastestMirror)
		if err != nil {
			log.Error().Err(err).Str("distro", name).Msg("failed to parse fastest mirror URL")
			return
		}

		// Update the rewriter with the new fastest mirror
		rewriters.Mu.Lock()
		defer rewriters.Mu.Unlock()

		switch mode {
		case define.TYPE_LINUX_DISTROS_UBUNTU:
			if rewriters.Ubuntu != nil {
				rewriters.Ubuntu.mirror = parsedMirror
			}
		case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
			if rewriters.UbuntuPorts != nil {
				rewriters.UbuntuPorts.mirror = parsedMirror
			}
		case define.TYPE_LINUX_DISTROS_DEBIAN:
			if rewriters.Debian != nil {
				rewriters.Debian.mirror = parsedMirror
			}
		case define.TYPE_LINUX_DISTROS_CENTOS:
			if rewriters.Centos != nil {
				rewriters.Centos.mirror = parsedMirror
			}
		case define.TYPE_LINUX_DISTROS_ALPINE:
			if rewriters.Alpine != nil {
				rewriters.Alpine.mirror = parsedMirror
			}
		}

		log.Info().Str("distro", name).Str("mirror", result.FastestMirror).Msg("async benchmark completed, mirror updated")
	})

	return rewriter
}

// CreateNewRewriters initializes rewriters based on mode.
// This uses synchronous benchmark which may block startup for up to 30 seconds.
// For faster startup, use CreateNewRewritersAsync instead.
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

// CreateNewRewritersAsync initializes rewriters based on mode using async benchmark.
// This allows the server to start immediately with default mirrors while benchmark
// runs in the background. Once benchmark completes, mirrors are automatically updated.
// This is the recommended method for production use to minimize startup time.
func CreateNewRewritersAsync(mode int) *URLRewriters {
	rewriters := &URLRewriters{}

	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriters.Ubuntu = createRewriterAsync(mode, rewriters)
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriters.UbuntuPorts = createRewriterAsync(mode, rewriters)
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriters.Debian = createRewriterAsync(mode, rewriters)
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriters.Centos = createRewriterAsync(mode, rewriters)
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriters.Alpine = createRewriterAsync(mode, rewriters)
	default:
		rewriters.Ubuntu = createRewriterAsync(define.TYPE_LINUX_DISTROS_UBUNTU, rewriters)
		rewriters.UbuntuPorts = createRewriterAsync(define.TYPE_LINUX_DISTROS_UBUNTU_PORTS, rewriters)
		rewriters.Debian = createRewriterAsync(define.TYPE_LINUX_DISTROS_DEBIAN, rewriters)
		rewriters.Centos = createRewriterAsync(define.TYPE_LINUX_DISTROS_CENTOS, rewriters)
		rewriters.Alpine = createRewriterAsync(define.TYPE_LINUX_DISTROS_ALPINE, rewriters)
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
// It clears the benchmark cache to force fresh benchmark tests.
//
// IMPORTANT: This function creates new rewriters outside the lock to avoid
// blocking request processing during potentially slow network operations
// (benchmark tests). The lock is only held briefly during the pointer swap.
func RefreshRewriters(rewriters *URLRewriters, mode int) {
	if rewriters == nil {
		return
	}

	log := logger.Default()
	log.Info().Msg("refreshing mirror configurations...")

	// Clear benchmark cache to force fresh tests
	benchmarks.ClearBenchmarkCache()

	// Create new rewriters OUTSIDE the lock to avoid blocking requests
	// during potentially slow network operations (benchmark tests)
	var (
		newUbuntu      *URLRewriter
		newUbuntuPorts *URLRewriter
		newDebian      *URLRewriter
		newCentos      *URLRewriter
		newAlpine      *URLRewriter
	)

	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		newUbuntu = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		newUbuntuPorts = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		newDebian = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_CENTOS:
		newCentos = createRewriter(mode)
	case define.TYPE_LINUX_DISTROS_ALPINE:
		newAlpine = createRewriter(mode)
	default:
		newUbuntu = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU)
		newUbuntuPorts = createRewriter(define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
		newDebian = createRewriter(define.TYPE_LINUX_DISTROS_DEBIAN)
		newCentos = createRewriter(define.TYPE_LINUX_DISTROS_CENTOS)
		newAlpine = createRewriter(define.TYPE_LINUX_DISTROS_ALPINE)
	}

	// Only hold the lock briefly during the pointer swap
	rewriters.Mu.Lock()
	switch mode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		rewriters.Ubuntu = newUbuntu
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		rewriters.UbuntuPorts = newUbuntuPorts
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		rewriters.Debian = newDebian
	case define.TYPE_LINUX_DISTROS_CENTOS:
		rewriters.Centos = newCentos
	case define.TYPE_LINUX_DISTROS_ALPINE:
		rewriters.Alpine = newAlpine
	default:
		rewriters.Ubuntu = newUbuntu
		rewriters.UbuntuPorts = newUbuntuPorts
		rewriters.Debian = newDebian
		rewriters.Centos = newCentos
		rewriters.Alpine = newAlpine
	}
	rewriters.Mu.Unlock()

	log.Info().Msg("mirror configurations refreshed successfully")
}
