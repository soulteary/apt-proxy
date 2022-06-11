package linux

import (
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

func benchmark(base string, query string, times int) (time.Duration, error) {
	var sum int64
	var d time.Duration
	url := base + query

	timeout := time.Duration(mirrorTimeout * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	for i := 0; i < times; i++ {
		timer := time.Now()
		response, err := client.Get(url)
		if err != nil {
			return d, err
		}

		defer response.Body.Close()
		_, err = ioutil.ReadAll(response.Body)
		if err != nil {
			return d, err
		}

		sum = sum + int64(time.Since(timer))
	}

	return time.Duration(sum / int64(times)), nil
}

type benchmarkResult struct {
	URL      string
	Duration time.Duration
}

func fastest(m Mirrors, testUrl string) (string, error) {
	ch := make(chan benchmarkResult)
	log.Printf("Start benchmarking mirrors")
	// kick off all benchmarks in parallel
	for _, url := range m.URLs {
		go func(u string) {
			duration, err := benchmark(u, testUrl, benchmarkTimes)
			if err == nil {
				ch <- benchmarkResult{u, duration}
			}
		}(url)
	}

	readN := len(m.URLs)
	if 3 < readN {
		readN = 3
	}

	// wait for the fastest results to come back
	results, err := readResults(ch, readN)
	log.Printf("Finished benchmarking mirrors")
	if len(results) == 0 {
		return "", errors.New("No results found: " + err.Error())
	} else if err != nil {
		log.Printf("Error benchmarking mirrors: %s", err.Error())
	}

	return results[0].URL, nil
}

func readResults(ch <-chan benchmarkResult, size int) (br []benchmarkResult, err error) {
	for {
		select {
		case r := <-ch:
			br = append(br, r)
			if len(br) >= size {
				return br, nil
			}
		case <-time.After(benchmarkTimeout * time.Second):
			return br, errors.New("Timed out waiting for results")
		}
	}
}
