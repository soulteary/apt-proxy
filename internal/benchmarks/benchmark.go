package benchmarks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	logger "github.com/soulteary/logger-kit"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Discard body but handle potential errors
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("non-200 status code received")
	}

	return time.Since(start), nil
}

// GetTheFastestMirror finds the fastest responding mirror from the provided list
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

	// Launch benchmarks in parallel
	for _, url := range mirrors {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			duration, err := Benchmark(ctx, u, testURL, BenchmarkMaxTries)
			if err != nil {
				errs <- err
				return
			}
			results <- Result{URL: u, Duration: duration}
		}(url)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
		close(errs)
	}()

	// Collect and sort results
	var collectedResults Results
	for result := range results {
		collectedResults = append(collectedResults, result)
		if len(collectedResults) >= maxResults {
			break
		}
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetTheFastestMirrorWithCache finds the fastest mirror, using cache if available.
// This is the preferred method for production use as it avoids repeated benchmarking.
func GetTheFastestMirrorWithCache(distType int, mirrors []string, testURL string) (string, error) {
	// Check cache first
	if cached, ok := defaultCache.GetCachedResult(distType); ok {
		log := logger.Default()
		log.Debug().Int("dist_type", distType).Str("mirror", cached).Msg("using cached benchmark result")
		return cached, nil
	}

	// Run benchmark
	fastest, err := GetTheFastestMirror(mirrors, testURL)
	if err != nil {
		return "", err
	}

	// Cache the result
	defaultCache.SetCachedResult(distType, fastest, DefaultCacheTTL)
	return fastest, nil
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
