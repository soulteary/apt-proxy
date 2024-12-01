package benchmarks

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	// Configuration constants
	BenchmarkMaxTimeout    = 150 * time.Second // detect resource timeout
	BenchmarkMaxTries      = 3                 // maximum number of attempts
	BenchmarkDetectTimeout = 30 * time.Second  // for select fast mirror
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

// Benchmark performs HTTP GET requests and measures response time
func Benchmark(ctx context.Context, base, query string, times int) (time.Duration, error) {
	client := &http.Client{
		Timeout: BenchmarkMaxTimeout,
		Transport: &http.Transport{
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
		},
	}

	var totalDuration time.Duration
	for i := 0; i < times; i++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
			duration, err := singleBenchmark(ctx, client, base+query)
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
	defer resp.Body.Close()

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
	ctx, cancel := context.WithTimeout(context.Background(), BenchmarkDetectTimeout)
	defer cancel()

	maxResults := min(len(mirrors), 3)
	results := make(chan Result, len(mirrors))
	var wg sync.WaitGroup

	// Create error channel to collect errors
	errs := make(chan error, len(mirrors))

	log.Printf("Starting benchmark for %d mirrors", len(mirrors))

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
	log.Printf("Completed benchmark. Found %d valid results", len(collectedResults))

	return collectedResults[0].URL, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
