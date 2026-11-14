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

package benchmarks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/logsafe"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	logger "github.com/soulteary/logger-kit/v3"
	"golang.org/x/sync/singleflight"
)

// BenchmarkCache stores cached benchmark results to avoid repeated testing.
// Results are cached per distribution type with TTL-based expiration.
type BenchmarkCache struct {
	mu      sync.RWMutex
	results map[int]CachedResult
}

// NewBenchmarkCache returns a fresh, empty cache.
func NewBenchmarkCache() *BenchmarkCache {
	return &BenchmarkCache{results: make(map[int]CachedResult)}
}

// CachedResult represents a cached benchmark result with expiration.
type CachedResult struct {
	FastestMirror string
	CachedAt      time.Time
	TTL           time.Duration
}

// IsExpired returns true if the cached result has expired.
func (c CachedResult) IsExpired() bool {
	return time.Since(c.CachedAt) > c.TTL
}

// DefaultCacheTTL is the default time-to-live for cached benchmark results.
const DefaultCacheTTL = 24 * time.Hour

// GetCachedResult returns a cached benchmark result if available and not expired.
func (bc *BenchmarkCache) GetCachedResult(distType int) (string, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	result, exists := bc.results[distType]
	if !exists || result.IsExpired() {
		return "", false
	}
	return result.FastestMirror, true
}

// SetCachedResult stores a benchmark result in the cache.
func (bc *BenchmarkCache) SetCachedResult(distType int, fastestMirror string, ttl time.Duration) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.results[distType] = CachedResult{
		FastestMirror: fastestMirror,
		CachedAt:      time.Now(),
		TTL:           ttl,
	}
}

// ClearCache clears all cached benchmark results.
func (bc *BenchmarkCache) ClearCache() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.results = make(map[int]CachedResult)
}

// AsyncBenchmarkResult represents the result of an async benchmark operation.
type AsyncBenchmarkResult struct {
	DistType      int
	FastestMirror string
	Error         error
}

// AsyncBenchmarkCallback is called when an async benchmark completes.
type AsyncBenchmarkCallback func(result AsyncBenchmarkResult)

const (
	// Configuration constants
	BenchmarkMaxTimeout    = 150 * time.Second // detect resource timeout
	BenchmarkMaxTries      = 3                 // maximum number of attempts
	BenchmarkDetectTimeout = 30 * time.Second  // for select fast mirror
	BenchmarkProbeTimeout  = 5 * time.Second   // for one attempt against one mirror
)

// MaxBenchmarkConcurrency caps how many mirror benchmarks run in parallel.
// Mirror lists can have 50+ entries; spawning that many concurrent TCP
// connections wastes resources and can trip rate limits on shared CDNs.
const MaxBenchmarkConcurrency = 8

// newBenchmarkClient builds the HTTP client an Engine probes mirrors with. The
// settings (connection pool, timeouts) are kept on a fresh client per Engine
// so two engines do not share TCP connection state or mutate each other's
// transport.
func newBenchmarkClient() *http.Client {
	return &http.Client{
		Timeout: BenchmarkMaxTimeout,
		// The reverse proxy returns upstream redirects to the package manager
		// instead of following them, so a probe has to see the 3xx a client
		// would get rather than the page it leads to.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			// Mirror election has to reach the outside world the same way
			// the proxy itself does. Without this, a deployment that only
			// reaches mirrors through a forward proxy benchmarks every
			// candidate to a timeout and elects a mirror it cannot use.
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			// Limit response header timeout for faster failure detection
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// Engine encapsulates everything that used to live as package-level state:
// the result cache, the singleflight group that collapses concurrent
// cache-miss requests, and the HTTP clients used to probe and discover mirrors.
//
// Each Server should construct its own Engine via NewEngine so a refresh on
// one Server does not flush another Server's mirror selection cache. The
// package-level helpers (GetTheFastestMirror, ClearBenchmarkCache, ...) are
// thin wrappers around a process-wide Default() Engine, kept for backward
// compatibility with existing tests and any single-Server callers.
type Engine struct {
	cache *BenchmarkCache
	group singleflight.Group
	// client probes candidates and never follows redirects.
	client *http.Client
	// discovery fetches the geo mirror list of a guarded engine. The endpoint
	// may redirect, so this client follows redirects; it shares client's
	// transport, so the upstream policy checks every hop.
	discovery *http.Client
	guarded   bool
}

// NewEngine returns a fresh, independent Engine. Use one per Server.
func NewEngine() *Engine { return NewEngineWithPolicy(nil) }

// NewEngineWithPolicy applies the daemon's immutable upstream policy to mirror
// probes, including their HEAD-to-GET fallback, and to geo discovery,
// including every redirect it follows.
func NewEngineWithPolicy(policy *access.Upstream) *Engine {
	client := newBenchmarkClient()
	if policy != nil {
		client.Transport = policy.Transport(client.Transport.(*http.Transport))
	}
	discovery := &http.Client{Timeout: client.Timeout, Transport: client.Transport}
	return &Engine{cache: NewBenchmarkCache(), client: client, discovery: discovery, guarded: policy != nil}
}

// MirrorCandidates discovers mirrors through this engine's policy. Legacy
// engines preserve the existing lookup path; guarded engines never fall back
// to an unguarded discovery client.
func (e *Engine) MirrorCandidates(reg *distro.Registry, mode int) []string {
	if !e.guarded {
		return mirrors.GetGeoMirrorUrlsByMode(reg, mode)
	}
	return mirrors.GetGeoMirrorUrlsByModeWithLookup(reg, mode, func() ([]string, error) {
		return mirrors.GetUbuntuMirrorUrlsByGeoWithClient(context.Background(), e.discovery)
	})
}

// Cache exposes the engine's result cache for advanced callers / tests.
func (e *Engine) Cache() *BenchmarkCache {
	return e.cache
}

// ClearCache drops all cached benchmark results held by this engine.
func (e *Engine) ClearCache() {
	e.cache.ClearCache()
}

// defaultEngine is the process-wide engine used by the package-level helper
// functions. New code should prefer constructing its own Engine.
var defaultEngine = NewEngine()

// Default returns the process-wide engine used by the package-level helpers.
func Default() *Engine {
	return defaultEngine
}

// Result stores benchmark results for a URL
type Result struct {
	URL      string
	Duration time.Duration
}

// Results implements sort.Interface for []Result based on Duration. Equal
// durations are ordered by URL so the order never depends on arrival order.
type Results []Result

func (r Results) Len() int { return len(r) }
func (r Results) Less(i, j int) bool {
	if r[i].Duration != r[j].Duration {
		return r[i].Duration < r[j].Duration
	}
	return r[i].URL < r[j].URL
}
func (r Results) Swap(i, j int) { r[i], r[j] = r[j], r[i] }

// Benchmark probes a mirror directory and relative path using the engine's
// probe client and reports the fastest of the given number of attempts. Later
// attempts can reuse the connection the first one opened, so the minimum
// measures the request itself rather than connection setup. One failed attempt
// fails the mirror.
func (e *Engine) Benchmark(ctx context.Context, base, query string, times int) (time.Duration, error) {
	var fastest time.Duration
	// Configuration may omit the root's final slash or include the probe's
	// leading slash. Keep mirror election consistent with request rewriting.
	probeURL := base
	if query != "" {
		probeURL = strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(query, "/")
	}
	for i := 0; i < times; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
			duration, err := singleBenchmark(ctx, e.client, probeURL)
			if err != nil {
				return 0, err
			}
			if i == 0 || duration < fastest {
				fastest = duration
			}
		}
	}

	return fastest, nil
}

// Benchmark is the package-level shim that delegates to the default engine.
func Benchmark(ctx context.Context, base, query string, times int) (time.Duration, error) {
	return defaultEngine.Benchmark(ctx, base, query, times)
}

func singleBenchmark(ctx context.Context, client *http.Client, target string) (time.Duration, error) {
	// An unresponsive mirror gives its concurrency slot back after this long
	// instead of holding it until the election deadline.
	ctx, cancel := context.WithTimeout(ctx, BenchmarkProbeTimeout)
	defer cancel()

	// Use HEAD when the upstream supports it: we only care about latency,
	// not the response payload. Falling back to GET (with a tiny CopyN
	// drain) is necessary for mirrors that don't implement HEAD correctly,
	// since some return 405 / empty bodies that would otherwise mark the
	// mirror as unhealthy.
	op := "Head"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		_ = resp.Body.Close()
		// Retry as GET, draining only the first 8 KiB to keep network cost low.
		op = "Get"
		getReq, gerr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if gerr != nil {
			return 0, gerr
		}
		start = time.Now()
		resp, err = client.Do(getReq)
		if err != nil {
			return 0, err
		}
		defer func() { _ = resp.Body.Close() }()
		if _, err := io.CopyN(io.Discard, resp.Body, 8*1024); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
	} else {
		defer func() { _ = resp.Body.Close() }()
		// HEAD response: drain a small amount in case the server still sends
		// a tiny body (some mirrors do, against spec).
		_, _ = io.CopyN(io.Discard, resp.Body, 1024)
	}

	// Redirects are not followed, so a 3xx fails here like any other status.
	if resp.StatusCode != http.StatusOK {
		return 0, probeFailure(op, target, fmt.Sprintf("unexpected status %d", resp.StatusCode))
	}
	// Repository metadata is never HTML: a 200 page is a landing page or a
	// soft 404 served in place of the requested file.
	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType == "text/html" {
		return 0, probeFailure(op, target, "unexpected HTML response")
	}

	return time.Since(start), nil
}

// probeFailure reports why an answered probe disqualifies its mirror. Like the
// transport errors of net/http it is a *url.Error, so a joined election error
// names every candidate and logsafe.Error can redact their credentials.
func probeFailure(op, target, reason string) error {
	return &url.Error{Op: op, URL: target, Err: errors.New(reason)}
}

// GetTheFastestMirror probes every mirror in the provided list, at most
// MaxBenchmarkConcurrency at a time, and returns the best one by rankResults.
// When BenchmarkDetectTimeout expires, queued mirrors are skipped and running
// probes are cancelled, so the election is decided among the mirrors that had
// answered by then.
func (e *Engine) GetTheFastestMirror(mirrors []string, testURL string) (string, error) {
	log := logger.Default()
	ctx, cancel := context.WithTimeout(context.Background(), BenchmarkDetectTimeout)
	defer cancel()

	results := make(chan Result, len(mirrors))
	var wg sync.WaitGroup

	// Create error channel to collect errors
	errs := make(chan error, len(mirrors))

	log.Info().Int("count", len(mirrors)).Msg("starting benchmark for mirrors")

	// Concurrency-limit semaphore. When ctx expires, workers that haven't
	// started yet bail out cheaply.
	sem := make(chan struct{}, MaxBenchmarkConcurrency)

	for _, mirror := range mirrors {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			duration, err := e.Benchmark(ctx, u, testURL, BenchmarkMaxTries)
			if err != nil {
				errs <- err
				return
			}
			results <- Result{URL: u, Duration: duration}
		}(mirror)
	}

	// Both channels hold one slot per mirror, so no worker blocks on sending.
	wg.Wait()
	close(results)
	close(errs)

	var collectedResults Results
	for result := range results {
		collectedResults = append(collectedResults, result)
	}

	if len(collectedResults) == 0 {
		// Collect errors if no results
		var errMsgs []error
		for err := range errs {
			errMsgs = append(errMsgs, err)
		}
		if len(errMsgs) > 0 {
			return "", errors.Join(errMsgs...)
		}
		return "", errors.New("no valid results found")
	}

	log.Info().Int("valid_results", len(collectedResults)).Msg("completed benchmark")

	return rankResults(collectedResults)[0].URL, nil
}

// rankResults orders successful results for election. A mirror that answered
// over both schemes keeps only its https result, so it is used over plain http
// only when its https variant failed. The rest is sorted by duration, then by
// URL.
func rankResults(results Results) Results {
	secure := make(map[string]struct{}, len(results))
	for _, result := range results {
		if rest, ok := strings.CutPrefix(result.URL, "https://"); ok {
			secure[rest] = struct{}{}
		}
	}
	ranked := make(Results, 0, len(results))
	for _, result := range results {
		if rest, ok := strings.CutPrefix(result.URL, "http://"); ok {
			if _, both := secure[rest]; both {
				continue
			}
		}
		ranked = append(ranked, result)
	}
	sort.Sort(ranked)
	return ranked
}

// GetTheFastestMirror is the package-level shim that delegates to the default engine.
func GetTheFastestMirror(mirrors []string, testURL string) (string, error) {
	return defaultEngine.GetTheFastestMirror(mirrors, testURL)
}

// GetTheFastestMirrorWithCache finds the fastest mirror, using cache if
// available. This is the preferred method for production use as it avoids
// repeated benchmarking. Concurrent cache-miss callers for the same distType
// are collapsed into a single execution via the engine's singleflight group.
func (e *Engine) GetTheFastestMirrorWithCache(distType int, mirrors []string, testURL string) (string, error) {
	if cached, ok := e.cache.GetCachedResult(distType); ok {
		log := logger.Default()
		log.Debug().Int("dist_type", distType).Str("mirror", logsafe.URL(cached)).Msg("using cached benchmark result")
		return cached, nil
	}

	key := singleflightKey(distType)
	v, err, _ := e.group.Do(key, func() (interface{}, error) {
		// Re-check after acquiring the singleflight slot in case another
		// goroutine just populated the cache.
		if cached, ok := e.cache.GetCachedResult(distType); ok {
			return cached, nil
		}
		fastest, ferr := e.GetTheFastestMirror(mirrors, testURL)
		if ferr != nil {
			return "", ferr
		}
		e.cache.SetCachedResult(distType, fastest, DefaultCacheTTL)
		return fastest, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// GetTheFastestMirrorWithCache is the package-level shim that delegates to the
// default engine.
func GetTheFastestMirrorWithCache(distType int, mirrors []string, testURL string) (string, error) {
	return defaultEngine.GetTheFastestMirrorWithCache(distType, mirrors, testURL)
}

func singleflightKey(distType int) string {
	return "benchmark/" + strconv.Itoa(distType)
}

// GetTheFastestMirrorAsync runs benchmark in the background and calls the
// callback when complete. This allows the application to start immediately
// with a default mirror while benchmarking runs.
//
// Concurrent async calls for the same distType are collapsed via the
// engine's singleflight group: only one goroutine probes mirrors, and
// every caller's callback is invoked with the same result. This matches
// the deduplication behaviour of GetTheFastestMirrorWithCache so a SIGHUP
// reload that fires sync + async benchmarks in quick succession does not
// double-probe upstream mirrors.
func (e *Engine) GetTheFastestMirrorAsync(distType int, mirrors []string, testURL string, callback AsyncBenchmarkCallback) {
	log := logger.Default()

	go func() {
		if cached, ok := e.cache.GetCachedResult(distType); ok {
			log.Debug().Int("dist_type", distType).Str("mirror", logsafe.URL(cached)).Msg("async: using cached benchmark result")
			callback(AsyncBenchmarkResult{
				DistType:      distType,
				FastestMirror: cached,
				Error:         nil,
			})
			return
		}

		log.Info().Int("dist_type", distType).Msg("async: starting background benchmark")

		key := singleflightKey(distType)
		v, err, shared := e.group.Do(key, func() (interface{}, error) {
			// Re-check after acquiring the singleflight slot in case
			// another goroutine just populated the cache.
			if cached, ok := e.cache.GetCachedResult(distType); ok {
				return cached, nil
			}
			fastest, ferr := e.GetTheFastestMirror(mirrors, testURL)
			if ferr != nil {
				return "", ferr
			}
			e.cache.SetCachedResult(distType, fastest, DefaultCacheTTL)
			return fastest, nil
		})
		if err != nil {
			log.Error().Err(logsafe.Error(err)).Int("dist_type", distType).Bool("shared", shared).Msg("async: benchmark failed")
			callback(AsyncBenchmarkResult{
				DistType:      distType,
				FastestMirror: "",
				Error:         err,
			})
			return
		}

		fastest, _ := v.(string)
		log.Info().Int("dist_type", distType).Str("mirror", logsafe.URL(fastest)).Bool("shared", shared).Msg("async: benchmark completed")

		callback(AsyncBenchmarkResult{
			DistType:      distType,
			FastestMirror: fastest,
			Error:         nil,
		})
	}()
}

// GetTheFastestMirrorAsync is the package-level shim that delegates to the
// default engine.
func GetTheFastestMirrorAsync(distType int, mirrors []string, testURL string, callback AsyncBenchmarkCallback) {
	defaultEngine.GetTheFastestMirrorAsync(distType, mirrors, testURL, callback)
}

// GetDefaultMirror returns the first mirror from the list as a fallback.
// A distribution without an official address uses it while its async
// benchmark is in progress and after that benchmark failed.
func GetDefaultMirror(mirrors []string) string {
	if len(mirrors) == 0 {
		return ""
	}
	return mirrors[0]
}

// ClearBenchmarkCache clears the default engine's cached benchmark results.
// New, per-Server callers should use Engine.ClearCache instead.
func ClearBenchmarkCache() {
	defaultEngine.ClearCache()
}

// GetBenchmarkCache returns the default engine's benchmark cache.
// New, per-Server callers should use Engine.Cache instead.
func GetBenchmarkCache() *BenchmarkCache {
	return defaultEngine.cache
}
