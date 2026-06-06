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
	"sync"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/internal/state"
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

// distroDescriptor consolidates per-distro metadata that previously lived in
// multiple parallel maps (modeRules, rewriterConfigByMode, rewriterFieldByMode).
// Adding a new distro now means appending one entry to distroDescriptors.
type distroDescriptor struct {
	mode         int
	name         string
	defaultRules []distro.Rule
	getMirror    func(*state.AppState) *url.URL
	rewriter     func(*URLRewriters) **URLRewriter
}

var distroDescriptors = []distroDescriptor{
	{
		mode:         distro.TypeUbuntu,
		name:         "Ubuntu",
		defaultRules: distro.UbuntuDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeUbuntu) },
		rewriter:     func(r *URLRewriters) **URLRewriter { return &r.Ubuntu },
	},
	{
		mode:         distro.TypeUbuntuPorts,
		name:         "Ubuntu Ports",
		defaultRules: distro.UbuntuPortsDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeUbuntuPorts) },
		rewriter:     func(r *URLRewriters) **URLRewriter { return &r.UbuntuPorts },
	},
	{
		mode:         distro.TypeDebian,
		name:         "Debian",
		defaultRules: distro.DebianDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeDebian) },
		rewriter:     func(r *URLRewriters) **URLRewriter { return &r.Debian },
	},
	{
		mode:         distro.TypeCentOS,
		name:         "CentOS",
		defaultRules: distro.CentosDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeCentOS) },
		rewriter:     func(r *URLRewriters) **URLRewriter { return &r.Centos },
	},
	{
		mode:         distro.TypeAlpine,
		name:         "Alpine",
		defaultRules: distro.AlpineDefaultCacheRules,
		getMirror:    func(s *state.AppState) *url.URL { return s.GetMirror(distro.TypeAlpine) },
		rewriter:     func(r *URLRewriters) **URLRewriter { return &r.Alpine },
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

func modesToInit(mode int) []int {
	if mode == distro.TypeAllDistros {
		return distroModesOrder
	}
	return []int{mode}
}

func rewriterField(r *URLRewriters, mode int) **URLRewriter {
	if d, ok := descriptorByMode[mode]; ok {
		return d.rewriter(r)
	}
	return nil
}

func getRewriterConfig(mode int) (descriptor *distroDescriptor, name string) {
	d, ok := descriptorByMode[mode]
	if !ok {
		return nil, ""
	}
	return d, d.name
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
	d, name := getRewriterConfig(mode)
	if d == nil {
		return nil
	}

	benchmarkURL, pattern := mirrors.GetPredefinedConfiguration(reg, mode)
	rewriter := &URLRewriter{pattern: pattern}
	mirror := d.getMirror(st)

	if mirror != nil {
		log.Info().Str("distro", name).Str("mirror", mirror.String()).Msg("using specified mirror")
		rewriter.mirror = mirror
		return rewriter
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

	return rewriter
}

// createRewriterAsync creates a new URLRewriter for a specific distribution using async benchmark.
// It immediately returns with a default mirror and updates the mirror in the background.
func createRewriterAsync(mode int, st *state.AppState, reg *distro.Registry, rewriters *URLRewriters, bench *benchmarks.Engine) *URLRewriter {
	log := logger.Default()
	d, name := getRewriterConfig(mode)
	if d == nil {
		return nil
	}

	engine := benchEngine(bench)

	benchmarkURL, pattern := mirrors.GetPredefinedConfiguration(reg, mode)
	rewriter := &URLRewriter{pattern: pattern}
	mirror := d.getMirror(st)

	if mirror != nil {
		log.Info().Str("distro", name).Str("mirror", mirror.String()).Msg("using specified mirror")
		rewriter.mirror = mirror
		return rewriter
	}

	mirrorURLs := mirrors.GetGeoMirrorUrlsByMode(reg, mode)

	// Check if we have a cached result
	if cached, ok := engine.Cache().GetCachedResult(mode); ok {
		if parsedMirror, err := url.Parse(cached); err == nil {
			log.Info().Str("distro", name).Str("mirror", cached).Msg("using cached mirror")
			rewriter.mirror = parsedMirror
			return rewriter
		}
	}

	defaultMirror := benchmarks.GetDefaultMirror(mirrorURLs)
	if parsedMirror, err := url.Parse(defaultMirror); err == nil {
		log.Info().Str("distro", name).Str("mirror", defaultMirror).Msg("using default mirror (async benchmark pending)")
		rewriter.mirror = parsedMirror
	}

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
		p := rewriterField(rewriters, mode)
		if p == nil || *p == nil {
			rewriters.Mu.Unlock()
			return
		}
		// Build the replacement off the current snapshot's pattern so a
		// concurrent RefreshRewriters cannot accidentally lose its newer
		// pattern when this stale callback fires.
		oldPattern := (*p).pattern
		*p = &URLRewriter{mirror: parsedMirror, pattern: oldPattern}
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
	for _, m := range modesToInit(mode) {
		if p := rewriterField(rewriters, m); p != nil {
			*p = createRewriter(m, st, reg, bench)
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
	for _, m := range modesToInit(mode) {
		if p := rewriterField(rewriters, m); p != nil {
			*p = createRewriterAsync(m, st, reg, rewriters, bench)
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

	var rewriter *URLRewriter
	if p := rewriterField(rewriters, mode); p != nil {
		rewriter = *p
	}
	if rewriter == nil || rewriter.mirror == nil || rewriter.pattern == nil {
		return
	}

	uri := r.URL.String()
	matches := rewriter.pattern.FindStringSubmatch(uri)
	if len(matches) == 0 {
		return
	}

	queryRaw := matches[len(matches)-1]
	unescapedQuery, err := url.PathUnescape(queryRaw)
	if err != nil {
		logger.Default().Debug().Err(err).Str("query", queryRaw).Msg("path unescape failed, using raw value")
		unescapedQuery = queryRaw
	}

	r.URL.Scheme = rewriter.mirror.Scheme
	r.URL.Host = rewriter.mirror.Host
	r.URL.Path = rewriter.mirror.Path + unescapedQuery
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
	newByMode := make(map[int]*URLRewriter, len(distroModesOrder))
	for _, m := range modesToInit(mode) {
		newByMode[m] = createRewriter(m, st, reg, engine)
	}

	rewriters.Mu.Lock()
	for _, m := range modesToInit(mode) {
		if p := rewriterField(rewriters, m); p != nil {
			*p = newByMode[m]
		}
	}
	rewriters.Mu.Unlock()

	log.Info().Msg("mirror configurations refreshed successfully")
}
