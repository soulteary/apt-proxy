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
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	logger "github.com/soulteary/logger-kit"
	"golang.org/x/sync/singleflight"
)

// BenchmarkCache stores cached benchmark results to avoid repeated testing.
// Results are cached per distribution type with TTL-based expiration.
type BenchmarkCache struct {
	mu      sync.RWMutex
	results map[int]CachedResult
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

// defaultCache is the global benchmark cache instance.
var defaultCache = &BenchmarkCache{
	results: make(map[int]CachedResult),
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
)

var (
	// benchmarkClient is a shared HTTP client for all benchmark operations.
	// Using a shared client with connection pooling improves performance
	// by reusing TCP connections and reducing handshake overhead.
	benchmarkClient = &http.Client{
		Timeout: BenchmarkMaxTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			// Limit response header timeout for faster failure detection
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
)

// Result stores benchmark results for a URL
type Result struct {
	URL      string
	Duration time.Duration
}

// Results implements sort.Interface for []Result based on Duration
type Results []Result

func (r Results) Len() int           { return len(r) }
func (r Results) Less(i, j int) bool { return r[i].Duration < r[j].Duration }
func (r Results) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

// Benchmark performs HTTP GET requests and measures response time.
// It uses a shared HTTP client with connection pooling for better performance.
func Benchmark(ctx context.Context, base, query string, times int) (time.Duration, error) {
	var totalDuration time.Duration
	for i := 0; i < times; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
			duration, err := singleBenchmark(ctx, benchmarkClient, base+query)
			if err != nil {
				return 0, err
			}
			totalDuration += duration
		}
	}

	return totalDuration / time.Duration(times), nil
}

func singleBenchmark(ctx context.Context, client *http.Client, url string) (time.Duration, error) {
	// Use HEAD when the upstream supports it: we only care about latency,
	// not the response payload. Falling back to GET (with a tiny CopyN
	// drain) is necessary for mirrors that don't implement HEAD correctly,
	// since some return 405 / empty bodies that would otherwise mark the
	// mirror as unhealthy.
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
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
		getReq, gerr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("non-200 status code received")
	}

	return time.Since(start), nil
}

// MaxBenchmarkConcurrency caps how many mirror benchmarks run in parallel.
// Mirror lists can have 50+ entries; spawning that many concurrent TCP
// connections wastes resources and can trip rate limits on shared CDNs.
const MaxBenchmarkConcurrency = 8

// GetTheFastestMirror finds the fastest responding mirror from the provided list.
// Concurrency is capped at MaxBenchmarkConcurrency. Once `maxResults` valid
// results are collected, the parent context is cancelled so in-flight
// benchmarks abort promptly instead of running to completion.
func GetTheFastestMirror(mirrors []string, testURL string) (string, error) {
	log := logger.Default()
	ctx, cancel := context.WithTimeout(context.Background(), BenchmarkDetectTimeout)
	defer cancel()

	maxResults := min(len(mirrors), 3)
	results := make(chan Result, len(mirrors))
	var wg sync.WaitGroup

	// Create error channel to collect errors
	errs := make(chan error, len(mirrors))

	log.Info().Int("count", len(mirrors)).Msg("starting benchmark for mirrors")

	// Concurrency-limit semaphore. When ctx is cancelled (e.g. enough results
	// were collected), workers that haven't started yet bail out cheaply.
	sem := make(chan struct{}, MaxBenchmarkConcurrency)

	for _, url := range mirrors {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			duration, err := Benchmark(ctx, u, testURL, BenchmarkMaxTries)
			if err != nil {
				errs <- err
				return
			}
			results <- Result{URL: u, Duration: duration}
		}(url)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errs)
	}()

	var collectedResults Results
	brokeEarly := false
	for result := range results {
		collectedResults = append(collectedResults, result)
		if len(collectedResults) >= maxResults {
			// Signal the remaining workers to stop ASAP.
			cancel()
			brokeEarly = true
			break
		}
	}

	if brokeEarly {
		// Drain remaining channels to avoid goroutine leaks: workers are still
		// running and the closer goroutine is blocked on wg.Wait.
		go func() {
			for range results {
			}
			for range errs {
			}
		}()
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

	sort.Sort(collectedResults)
	log.Info().Int("valid_results", len(collectedResults)).Msg("completed benchmark")

	return collectedResults[0].URL, nil
}

// benchmarkGroup collapses concurrent cache-miss benchmark requests for the
// same distType into a single execution.
var benchmarkGroup singleflight.Group

// GetTheFastestMirrorWithCache finds the fastest mirror, using cache if available.
// This is the preferred method for production use as it avoids repeated benchmarking.
func GetTheFastestMirrorWithCache(distType int, mirrors []string, testURL string) (string, error) {
	// Check cache first
	if cached, ok := defaultCache.GetCachedResult(distType); ok {
		log := logger.Default()
		log.Debug().Int("dist_type", distType).Str("mirror", cached).Msg("using cached benchmark result")
		return cached, nil
	}

	key := singleflightKey(distType)
	v, err, _ := benchmarkGroup.Do(key, func() (interface{}, error) {
		// Re-check after acquiring the singleflight slot in case another
		// goroutine just populated the cache.
		if cached, ok := defaultCache.GetCachedResult(distType); ok {
			return cached, nil
		}
		fastest, ferr := GetTheFastestMirror(mirrors, testURL)
		if ferr != nil {
			return "", ferr
		}
		defaultCache.SetCachedResult(distType, fastest, DefaultCacheTTL)
		return fastest, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func singleflightKey(distType int) string {
	return "benchmark/" + strconv.Itoa(distType)
}

// GetTheFastestMirrorAsync runs benchmark in the background and calls the callback when complete.
// This allows the application to start immediately with a default mirror while benchmarking runs.
// The callback will be called with the result when the benchmark completes.
func GetTheFastestMirrorAsync(distType int, mirrors []string, testURL string, callback AsyncBenchmarkCallback) {
	log := logger.Default()

	go func() {
		// Check cache first
		if cached, ok := defaultCache.GetCachedResult(distType); ok {
			log.Debug().Int("dist_type", distType).Str("mirror", cached).Msg("async: using cached benchmark result")
			callback(AsyncBenchmarkResult{
				DistType:      distType,
				FastestMirror: cached,
				Error:         nil,
			})
			return
		}

		log.Info().Int("dist_type", distType).Msg("async: starting background benchmark")

		fastest, err := GetTheFastestMirror(mirrors, testURL)
		if err != nil {
			log.Error().Err(err).Int("dist_type", distType).Msg("async: benchmark failed")
			callback(AsyncBenchmarkResult{
				DistType:      distType,
				FastestMirror: "",
				Error:         err,
			})
			return
		}

		// Cache the result
		defaultCache.SetCachedResult(distType, fastest, DefaultCacheTTL)
		log.Info().Int("dist_type", distType).Str("mirror", fastest).Msg("async: benchmark completed")

		callback(AsyncBenchmarkResult{
			DistType:      distType,
			FastestMirror: fastest,
			Error:         nil,
		})
	}()
}

// GetDefaultMirror returns the first mirror from the list as a fallback.
// This is used when async benchmark is in progress or when benchmark fails.
func GetDefaultMirror(mirrors []string) string {
	if len(mirrors) == 0 {
		return ""
	}
	return mirrors[0]
}

// ClearBenchmarkCache clears all cached benchmark results.
// This is useful when forcing a re-benchmark, e.g., on SIGHUP reload.
func ClearBenchmarkCache() {
	defaultCache.ClearCache()
}

// GetBenchmarkCache returns the global benchmark cache instance.
// This can be used for testing or advanced cache management.
func GetBenchmarkCache() *BenchmarkCache {
	return defaultCache
}
