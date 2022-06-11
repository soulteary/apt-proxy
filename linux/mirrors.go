package linux

import (
	"bufio"
	"errors"
	"log"
	"net/http"
	"time"
)

type benchmarkResult struct {
	URL      string
	Duration time.Duration
}

type Mirrors struct {
	URLs []string
}

func GetGeoMirrors() (m Mirrors, err error) {
	response, err := http.Get(mirrorsUrl)
	if err != nil {
		return
	}

	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	m.URLs = []string{}

	// read urls line by line
	for scanner.Scan() {
		m.URLs = append(m.URLs, scanner.Text())
	}

	return m, scanner.Err()
}

func (m Mirrors) Fastest() (string, error) {
	ch := make(chan benchmarkResult)
	log.Printf("Start benchmarking mirrors")
	// kick off all benchmarks in parallel
	for _, url := range m.URLs {
		go func(u string) {
			duration, err := benchmark(u, benchmarkUrl, benchmarkTimes)
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
	results, err := m.readResults(ch, readN)
	log.Printf("Finished benchmarking mirrors")
	if len(results) == 0 {
		return "", errors.New("No results found: " + err.Error())
	} else if err != nil {
		log.Printf("Error benchmarking mirrors: %s", err.Error())
	}

	return results[0].URL, nil
}

func (m Mirrors) readResults(ch <-chan benchmarkResult, size int) (br []benchmarkResult, err error) {
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
