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

// Package proxy provides URL rewriting and reverse proxy functionality for apt-proxy.
// It handles distribution-specific URL patterns and routes requests to configured mirrors.
package proxy

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/internal/state"
)

// URLRewriter holds the mirror and pattern for URL rewriting
type URLRewriter struct {
	mirror         *url.URL
	securityMirror *url.URL
	pattern        *regexp.Regexp
	// hostPattern matches the request Host for archives served from the host
	// root. When it matches (and pattern did not), the whole request path is
	// the mirror-relative suffix.
	hostPattern *regexp.Regexp
	// securityHostPattern is the built-in Debian security host matcher. Only a
	// match on *this* selects securityMirror: a distributions.yaml entry may
	// point type 3 at some other host-root archive, and that one must resolve
	// to its configured mirror, not to the derived /debian-security/ path.
	securityHostPattern *regexp.Regexp
}

// hostPatternForMode returns the Host matcher for a distribution, preferring
// the registry entry so distributions.yaml can define one.
func hostPatternForMode(reg *distro.Registry, mode int) *regexp.Regexp {
	if reg != nil {
		if d, ok := reg.GetByType(mode); ok {
			return d.HostPattern
		}
	}
	return distro.BuiltinHostPattern(mode)
}

func debianSecurityMirror(archive, configured *url.URL, configuredAlias bool) *url.URL {
	if configured != nil && !configuredAlias {
		security := *configured
		if security.RawPath != "" {
			security.RawPath = strings.TrimSuffix(security.RawPath, "/") + "/"
			if decoded, err := url.PathUnescape(security.RawPath); err == nil {
				security.Path = decoded
			} else {
				security.RawPath = ""
				security.Path = strings.TrimSuffix(security.Path, "/") + "/"
			}
		} else {
			security.Path = strings.TrimSuffix(security.Path, "/") + "/"
		}
		return &security
	}
	source := archive
	if configured != nil {
		source = configured
	}
	if source == nil {
		return nil
	}

	security := *source
	path := strings.TrimSuffix(security.Path, "/")
	switch {
	case strings.HasSuffix(path, "/debian-security"):
		// Already points at the security archive.
	case strings.HasSuffix(path, "/debian"):
		path = strings.TrimSuffix(path, "/debian") + "/debian-security"
	default:
		path += "/debian-security"
	}
	security.Path = path + "/"
	security.RawPath = ""
	return &security
}

func attachDebianSecurityMirror(mode int, st *state.AppState, rewriter *URLRewriter) *URLRewriter {
	if mode == distro.TypeDebian && rewriter != nil {
		rewriter.securityMirror = debianSecurityMirror(
			rewriter.mirror,
			st.GetDebianSecurityMirror(),
			st.DebianSecurityMirrorResolvedAlias(),
		)
	}
	return rewriter
}

// URLRewriters manages rewriters for different distributions.
//
// The five built-in distros keep dedicated fields; distributions registered
// from distributions.yaml (any type outside the built-in set) live in the
// custom map so they get a rewriter too. Use get/set rather than touching
// either storage directly -- they keep the two halves interchangeable.
type URLRewriters struct {
	Ubuntu      *URLRewriter
	UbuntuPorts *URLRewriter
	Debian      *URLRewriter
	Centos      *URLRewriter
	Alpine      *URLRewriter

	// custom holds rewriters for registry-defined distributions keyed by
	// distro type. Guarded by Mu, like the named fields above.
	custom map[int]*URLRewriter

	Mu sync.RWMutex
}

// get returns the rewriter registered for mode, or nil when there is none.
// Callers must hold Mu (read or write) for concurrent use.
func (r *URLRewriters) get(mode int) *URLRewriter {
	if r == nil {
		return nil
	}
	if p := builtinField(r, mode); p != nil {
		return *p
	}
	return r.custom[mode]
}

// set stores v as the rewriter for mode, allocating the custom map on first
// use. Callers must hold Mu for writing.
func (r *URLRewriters) set(mode int, v *URLRewriter) {
	if r == nil {
		return
	}
	if p := builtinField(r, mode); p != nil {
		*p = v
		return
	}
	if r.custom == nil {
		r.custom = make(map[int]*URLRewriter, 1)
	}
	r.custom[mode] = v
}

// distroDescriptor consolidates per-distro metadata that previously lived in
// multiple parallel maps (modeRules, rewriterConfigByMode, rewriterFieldByMode).
// Adding a new distro now means appending one entry to distroDescriptors.
type distroDescriptor struct {
	mode         int
	name         string
	defaultRules []distro.Rule
	getMirror    func(*state.AppState) *url.URL
}

var distroDescriptors = []distroDescriptor{
	{
		mode:         distro.TypeUbuntu,
		name:         "Ubuntu",
		defaultRules: distro.UbuntuDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeUbuntu) },
	},
	{
		mode:         distro.TypeUbuntuPorts,
		name:         "Ubuntu Ports",
		defaultRules: distro.UbuntuPortsDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeUbuntuPorts) },
	},
	{
		mode:         distro.TypeDebian,
		name:         "Debian",
		defaultRules: distro.DebianDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeDebian) },
	},
	{
		mode:         distro.TypeCentOS,
		name:         "CentOS",
		defaultRules: distro.CentosDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeCentOS) },
	},
	{
		mode:         distro.TypeAlpine,
		name:         "Alpine",
		defaultRules: distro.AlpineDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeAlpine) },
	},
}

// descriptorByMode is a fast lookup index for distroDescriptors. Built once
// at package init so callers don't re-scan the slice.
var descriptorByMode = func() map[int]*distroDescriptor {
	m := make(map[int]*distroDescriptor, len(distroDescriptors))
	for i := range distroDescriptors {
		d := &distroDescriptors[i]
		m[d.mode] = d
	}
	return m
}()

// distroModesOrder lists known distro modes in registration order. Used to
// keep host-pattern matching deterministic across builds.
var distroModesOrder = func() []int {
	out := make([]int, 0, len(distroDescriptors))
	for _, d := range distroDescriptors {
		out = append(out, d.mode)
	}
	return out
}()

// modesToInit lists the distro types to build rewriters for. For
// TypeAllDistros this is the built-in set plus every additional type the
// registry knows about (distributions.yaml entries), so custom distros are
// initialised alongside the built-ins. Custom types are sorted to keep
// construction order deterministic across reloads.
func modesToInit(mode int, reg *distro.Registry) []int {
	if mode != distro.TypeAllDistros {
		return []int{mode}
	}
	out := append([]int(nil), distroModesOrder...)
	if reg == nil {
		return out
	}
	seen := make(map[int]struct{}, len(out))
	for _, m := range out {
		seen[m] = struct{}{}
	}
	var extra []int
	for _, d := range reg.GetAll() {
		if d.Type == distro.TypeAllDistros || d.URLPattern == nil {
			continue
		}
		if _, ok := seen[d.Type]; ok {
			continue
		}
		seen[d.Type] = struct{}{}
		extra = append(extra, d.Type)
	}
	sort.Ints(extra)
	return append(out, extra...)
}

// builtinField returns the dedicated struct field backing a built-in distro
// type, or nil when mode is not one of the compile-time built-ins.
func builtinField(r *URLRewriters, mode int) **URLRewriter {
	switch mode {
	case distro.TypeUbuntu:
		return &r.Ubuntu
	case distro.TypeUbuntuPorts:
		return &r.UbuntuPorts
	case distro.TypeDebian:
		return &r.Debian
	case distro.TypeCentOS:
		return &r.Centos
	case distro.TypeAlpine:
		return &r.Alpine
	default:
		return nil
	}
}

// resolveDescriptor returns the descriptor driving rewriter construction for
// mode. Built-in distros come from the compile-time table; anything else is
// synthesised from the registry, so a distribution added through
// distributions.yaml gets a rewriter (and therefore mirror rewriting) exactly
// like a built-in one. Returns nil when mode is unknown to both.
func resolveDescriptor(mode int, reg *distro.Registry) (descriptor *distroDescriptor, name string) {
	if d, ok := descriptorByMode[mode]; ok {
		return d, d.name
	}
	if reg == nil {
		return nil, ""
	}
	rd, ok := reg.GetByType(mode)
	if !ok || rd.URLPattern == nil {
		return nil, ""
	}
	label := rd.Name
	if label == "" {
		label = rd.ID
	}
	return &distroDescriptor{
		mode:         mode,
		name:         label,
		defaultRules: rd.CacheRules,
		// Custom distros have no dedicated CLI/env mirror flag, so AppState
		// has no slot for them; GetMirror returns nil and selection falls
		// through to the benchmark over the configured mirror list.
		getMirror: func(s *state.AppState) *url.URL { return s.GetMirror(mode) },
	}, label
}

// benchEngine resolves the *benchmarks.Engine to use. A nil engine falls back
// to benchmarks.Default() so legacy callers (and the existing rewriter unit
// tests that don't construct a PackageStruct) keep their previous behaviour.
func benchEngine(e *benchmarks.Engine) *benchmarks.Engine {
	if e != nil {
		return e
	}
	return benchmarks.Default()
}

// createRewriter creates a new URLRewriter for a specific distribution.
// It uses the cached benchmark result if available, otherwise runs a synchronous benchmark.
func createRewriter(mode int, st *state.AppState, reg *distro.Registry, bench *benchmarks.Engine) *URLRewriter {
	log := logger.Default()
	d, name := resolveDescriptor(mode, reg)
	if d == nil {
		return nil
	}

	benchmarkURL, pattern := mirrors.GetPredefinedConfiguration(reg, mode)
	rewriter := &URLRewriter{pattern: pattern, hostPattern: hostPatternForMode(reg, mode), securityHostPattern: distro.BuiltinHostPattern(mode)}
	mirror := d.getMirror(st)

	if mirror != nil {
		log.Info().Str("distro", name).Str("mirror", mirror.String()).Msg("using specified mirror")
		rewriter.mirror = mirror
		return attachDebianSecurityMirror(mode, st, rewriter)
	}

	mirrorURLs := mirrors.GetGeoMirrorUrlsByMode(reg, mode)
	// Use cache-aware benchmark to avoid repeated testing
	fastest, err := benchEngine(bench).GetTheFastestMirrorWithCache(mode, mirrorURLs, benchmarkURL)
	if err != nil {
		log.Error().Err(err).Str("distro", name).Msg("error finding fastest mirror")
		return rewriter
	}

	if mirror, err := url.Parse(fastest); err == nil {
		log.Info().Str("distro", name).Str("mirror", fastest).Msg("using fastest mirror")
		rewriter.mirror = mirror
	}

	return attachDebianSecurityMirror(mode, st, rewriter)
}

// createRewriterAsync creates a new URLRewriter for a specific distribution using async benchmark.
// It immediately returns with a default mirror and updates the mirror in the background.
func createRewriterAsync(mode int, st *state.AppState, reg *distro.Registry, rewriters *URLRewriters, bench *benchmarks.Engine) *URLRewriter {
	log := logger.Default()
	d, name := resolveDescriptor(mode, reg)
	if d == nil {
		return nil
	}

	engine := benchEngine(bench)

	benchmarkURL, pattern := mirrors.GetPredefinedConfiguration(reg, mode)
	rewriter := &URLRewriter{pattern: pattern, hostPattern: hostPatternForMode(reg, mode), securityHostPattern: distro.BuiltinHostPattern(mode)}
	mirror := d.getMirror(st)

	if mirror != nil {
		log.Info().Str("distro", name).Str("mirror", mirror.String()).Msg("using specified mirror")
		rewriter.mirror = mirror
		return attachDebianSecurityMirror(mode, st, rewriter)
	}

	mirrorURLs := mirrors.GetGeoMirrorUrlsByMode(reg, mode)

	// Check if we have a cached result
	if cached, ok := engine.Cache().GetCachedResult(mode); ok {
		if parsedMirror, err := url.Parse(cached); err == nil {
			log.Info().Str("distro", name).Str("mirror", cached).Msg("using cached mirror")
			rewriter.mirror = parsedMirror
			return attachDebianSecurityMirror(mode, st, rewriter)
		}
	}

	defaultMirror := benchmarks.GetDefaultMirror(mirrorURLs)
	if parsedMirror, err := url.Parse(defaultMirror); err == nil {
		log.Info().Str("distro", name).Str("mirror", defaultMirror).Msg("using default mirror (async benchmark pending)")
		rewriter.mirror = parsedMirror
	}
	attachDebianSecurityMirror(mode, st, rewriter)

	// Run benchmark in background and update when complete.
	//
	// Concurrency note: we *replace* the URLRewriter pointer in *p instead of
	// mutating the existing struct. Readers in RewriteRequestByMode (and
	// elsewhere) snapshot `*p` while holding rewriters.Mu.RLock and then
	// access mirror/pattern outside the lock; mutating in place would race
	// with those readers. Allocating a fresh URLRewriter and swapping the
	// pointer under rewriters.Mu.Lock keeps published structs immutable.
	engine.GetTheFastestMirrorAsync(mode, mirrorURLs, benchmarkURL, func(result benchmarks.AsyncBenchmarkResult) {
		if result.Error != nil {
			log.Error().Err(result.Error).Str("distro", name).Msg("async benchmark failed")
			return
		}

		parsedMirror, err := url.Parse(result.FastestMirror)
		if err != nil {
			log.Error().Err(err).Str("distro", name).Msg("failed to parse fastest mirror URL")
			return
		}

		rewriters.Mu.Lock()
		cur := rewriters.get(mode)
		if cur == nil {
			rewriters.Mu.Unlock()
			return
		}
		// Build the replacement off the current snapshot's pattern so a
		// concurrent RefreshRewriters cannot accidentally lose its newer
		// pattern when this stale callback fires.
		oldPattern := cur.pattern
		oldHostPattern := cur.hostPattern
		oldSecurityHostPattern := cur.securityHostPattern
		securityMirror := cur.securityMirror
		if mode == distro.TypeDebian && st.GetDebianSecurityMirror() == nil {
			securityMirror = debianSecurityMirror(parsedMirror, nil, false)
		}
		rewriters.set(mode, &URLRewriter{
			mirror:              parsedMirror,
			securityMirror:      securityMirror,
			pattern:             oldPattern,
			hostPattern:         oldHostPattern,
			securityHostPattern: oldSecurityHostPattern,
		})
		rewriters.Mu.Unlock()

		log.Info().Str("distro", name).Str("mirror", result.FastestMirror).Msg("async benchmark completed, mirror updated")
	})

	return rewriter
}

// CreateNewRewriters initializes rewriters based on mode using synchronous
// benchmark. May block startup for up to 30 seconds; prefer
// CreateNewRewritersAsync. Uses the process-wide default benchmarks.Engine;
// PackageStruct callers route through CreateNewRewritersWithEngine instead.
func CreateNewRewriters(mode int, st *state.AppState, reg *distro.Registry) *URLRewriters {
	return CreateNewRewritersWithEngine(mode, st, reg, nil)
}

// CreateNewRewritersWithEngine is the engine-aware variant of
// CreateNewRewriters. A nil engine falls back to benchmarks.Default().
func CreateNewRewritersWithEngine(mode int, st *state.AppState, reg *distro.Registry, bench *benchmarks.Engine) *URLRewriters {
	rewriters := &URLRewriters{}
	for _, m := range modesToInit(mode, reg) {
		if rw := createRewriter(m, st, reg, bench); rw != nil {
			rewriters.set(m, rw)
		}
	}
	return rewriters
}

// CreateNewRewritersAsync initializes rewriters based on mode using async benchmark.
// Recommended for production use to minimize startup time.
func CreateNewRewritersAsync(mode int, st *state.AppState, reg *distro.Registry) *URLRewriters {
	return CreateNewRewritersAsyncWithEngine(mode, st, reg, nil)
}

// CreateNewRewritersAsyncWithEngine is the engine-aware variant of
// CreateNewRewritersAsync. A nil engine falls back to benchmarks.Default().
func CreateNewRewritersAsyncWithEngine(mode int, st *state.AppState, reg *distro.Registry, bench *benchmarks.Engine) *URLRewriters {
	rewriters := &URLRewriters{}
	for _, m := range modesToInit(mode, reg) {
		if rw := createRewriterAsync(m, st, reg, rewriters, bench); rw != nil {
			rewriters.set(m, rw)
		}
	}
	return rewriters
}

// GetRewriteRulesByMode returns caching rules for a specific mode.
// Prefers registry (config-loaded) rules when present.
func GetRewriteRulesByMode(reg *distro.Registry, mode int) []distro.Rule {
	if reg != nil {
		if d, ok := reg.GetByType(mode); ok && len(d.CacheRules) > 0 {
			return d.CacheRules
		}
	}
	if d, ok := descriptorByMode[mode]; ok {
		return d.defaultRules
	}
	// Aggregate (TypeAllDistros and unknown modes): preserve descriptor order.
	n := 0
	for _, d := range distroDescriptors {
		n += len(d.defaultRules)
	}
	rules := make([]distro.Rule, 0, n)
	for _, d := range distroDescriptors {
		rules = append(rules, d.defaultRules...)
	}
	return rules
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

	rewriter := rewriters.get(mode)
	if rewriter == nil || rewriter.mirror == nil || rewriter.pattern == nil {
		return
	}

	// Match only the escaped path. URL.String also contains RawQuery; matching
	// it used to append the query to Path and then serialize RawQuery again.
	escapedPath := r.URL.EscapedPath()
	matches := matchDistroPath(rewriter.pattern, escapedPath)

	// matchedPath is the full distribution path selected by the rewrite
	// pattern; hostMatched records that we fell back to Host matching.
	host := requestHost(r)

	var suffixRaw, matchedPath string
	var hostMatched bool
	switch {
	case len(matches) > 0:
		suffixRaw = matches[len(matches)-1]
		matchedPath = matches[0]
	case rewriter.hostPattern != nil && rewriter.hostPattern.MatchString(host):
		// Archive served from the host root: the entire path is the
		// mirror-relative suffix.
		suffixRaw = strings.TrimPrefix(escapedPath, "/")
		hostMatched = true
	default:
		return
	}

	suffixPath, err := url.PathUnescape(suffixRaw)
	if err != nil {
		logger.Default().Debug().Err(err).Str("path", suffixRaw).Msg("path unescape failed, using raw value")
		suffixPath = suffixRaw
	}

	target := rewriter.mirror
	// Check matchedPath instead of the complete request path so
	// apt-cacher-ng-style host prefixes (for example
	// /security.debian.org/debian-security/...) still use the dedicated Debian
	// Security mirror. A Host match only counts when it is the built-in
	// security host: a YAML-configured host-root archive on type 3 belongs on
	// its own configured mirror.
	securityHostMatch := hostMatched &&
		rewriter.securityHostPattern != nil &&
		rewriter.securityHostPattern.MatchString(host)
	if mode == distro.TypeDebian && rewriter.securityMirror != nil &&
		(securityHostMatch || strings.HasPrefix(matchedPath, "/debian-security/")) {
		target = rewriter.securityMirror
	}

	r.URL.Scheme = target.Scheme
	r.URL.Host = target.Host
	r.URL.Path = target.Path + suffixPath
	rawPath := target.EscapedPath() + suffixRaw
	if rawPath != r.URL.Path {
		r.URL.RawPath = rawPath
	} else {
		r.URL.RawPath = ""
	}
}

// MatchingRule finds a matching rule for the given path
func MatchingRule(path string, rules []distro.Rule) (*distro.Rule, bool) {
	for _, rule := range rules {
		if rule.Pattern.MatchString(path) {
			return &rule, true
		}
	}
	return nil, false
}

// buildRewriters re-elects mirrors and returns a NEW rewriter set instead of
// mutating one in place, which is what lets PackageStruct.RefreshMirrors hold
// the finished set in hand and publish it together with the matching host
// patterns. RefreshRewritersWithEngine keeps the in-place behaviour for
// callers that own a set directly.
//
// A construction-time async benchmark that is still pending keeps writing into
// the set it captured, so its late result lands on a snapshot no longer
// serving traffic and is dropped. That is the right precedence: this
// synchronous election is both newer and complete.
func buildRewriters(mode int, st *state.AppState, reg *distro.Registry, bench *benchmarks.Engine) *URLRewriters {
	log := logger.Default()
	log.Info().Msg("refreshing mirror configurations...")

	engine := benchEngine(bench)
	engine.ClearCache()

	rewriters := &URLRewriters{}
	for _, m := range modesToInit(mode, reg) {
		if rw := createRewriter(m, st, reg, engine); rw != nil {
			rewriters.set(m, rw)
		}
	}

	log.Info().Msg("mirror configurations refreshed successfully")
	return rewriters
}

// RefreshRewriters refreshes the rewriters with updated mirror configurations.
// This function is safe to call concurrently and will update the mirrors
// based on the supplied AppState/Registry. It clears the process-wide
// default benchmark engine's cache to force fresh benchmark tests; callers
// holding a per-Server *benchmarks.Engine should prefer
// RefreshRewritersWithEngine so they only flush their own cache.
//
// IMPORTANT: This function creates new rewriters outside the lock to avoid
// blocking request processing during potentially slow network operations
// (benchmark tests). The lock is only held briefly during the pointer swap.
func RefreshRewriters(rewriters *URLRewriters, mode int, st *state.AppState, reg *distro.Registry) {
	RefreshRewritersWithEngine(rewriters, mode, st, reg, nil)
}

// RefreshRewritersWithEngine is the engine-aware variant of RefreshRewriters.
// A nil engine falls back to benchmarks.Default(). Only the supplied engine's
// cache is cleared; per-Server engines no longer interfere with each other.
func RefreshRewritersWithEngine(rewriters *URLRewriters, mode int, st *state.AppState, reg *distro.Registry, bench *benchmarks.Engine) {
	if rewriters == nil {
		return
	}

	log := logger.Default()
	log.Info().Msg("refreshing mirror configurations...")

	engine := benchEngine(bench)
	engine.ClearCache()

	// Create new rewriters OUTSIDE the lock to avoid blocking requests
	// during potentially slow network operations (benchmark tests)
	modes := modesToInit(mode, reg)
	newByMode := make(map[int]*URLRewriter, len(modes))
	for _, m := range modes {
		newByMode[m] = createRewriter(m, st, reg, engine)
	}

	rewriters.Mu.Lock()
	for _, m := range modes {
		rewriters.set(m, newByMode[m])
	}
	rewriters.Mu.Unlock()

	log.Info().Msg("mirror configurations refreshed successfully")
}
