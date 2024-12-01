package benchmarks

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

const (
	BENCHMARK_MAX_TIMEOUT    = 150 // 150 seconds, detect resource timeout
	BENCHMARK_MAX_TRIES      = 3   // times, maximum number of attempts
	BENCHMARK_DETECT_TIMEOUT = 30  // 30 seconds, for select fast mirror
)

type Result struct {
	URL      string
	Duration time.Duration
}

func Benchmark(base string, query string, times int) (time.Duration, error) {
	var sum int64

	timeout := time.Duration(BENCHMARK_MAX_TIMEOUT * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	for i := 0; i < times; i++ {
		timer := time.Now()
		response, err := client.Get(base + query)
		if err != nil {
			return timeout, err
		}

		defer response.Body.Close()
		_, err = io.ReadAll(response.Body)
		if err != nil {
			return timeout, err
		}

		sum = sum + int64(time.Since(timer))
	}

	return time.Duration(sum / int64(times)), nil
}

func GetTheFastestMirror(mirrors []string, testUrl string) (string, error) {
	ch := make(chan Result)
	log.Printf("Start benchmarking mirrors")
	// kick off all benchmarks in parallel
	for _, url := range mirrors {
		go func(u string) {
			duration, err := Benchmark(u, testUrl, BENCHMARK_MAX_TRIES)
			if err == nil {
				ch <- Result{u, duration}
			}
		}(url)
	}

	maxTries := len(mirrors)
	if maxTries > 3 {
		maxTries = 3
	}

	// wait for the fastest results to come back
	results, err := ReadResults(ch, maxTries)
	log.Printf("Finished benchmarking mirrors")
	if len(results) == 0 {
		return "", errors.New("No results found: " + err.Error())
	} else if err != nil {
		log.Printf("Error benchmarking mirrors: %s", err.Error())
	}

	return results[0].URL, nil
}

func ReadResults(ch <-chan Result, size int) (br []Result, err error) {
	for {
		select {
		case r := <-ch:
			br = append(br, r)
			if len(br) >= size {
				return br, nil
			}
		case <-time.After(BENCHMARK_DETECT_TIMEOUT * time.Second):
			return br, errors.New("timed out waiting for results")
		}
	}
}
