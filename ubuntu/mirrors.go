package ubuntu

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

const (
	mirrorsUrl       = "http://mirrors.ubuntu.com/mirrors.txt"
	benchmarkUrl     = "dists/jammy/main/binary-amd64/Release"
	benchmarkTimes   = 3
	benchmarkBytes   = 1024 * 512 // 512Kb
	benchmarkTimeout = 10         // 10 seconds
)

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

	// kick off all benchmarks in parallel
	for _, url := range m.URLs {
		go func(u string) {
			duration, err := m.benchmark(u, benchmarkTimes)
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
			if len(br) == size {
				return
			}
		case <-time.After(benchmarkTimeout * time.Second):
			return br, errors.New("Timed out waiting for results")
		}
	}
}

func (m Mirrors) benchmark(url string, times int) (time.Duration, error) {
	var sum int64
	var d time.Duration
	url = url + benchmarkUrl

	for i := 0; i < times; i++ {
		timer := time.Now()
		response, err := http.Get(url)
		if err != nil {
			return d, err
		}

		_, err = io.ReadAtLeast(response.Body, make([]byte, benchmarkBytes), benchmarkBytes)
		if err != nil {
			return d, err
		}

		sum = sum + int64(time.Since(timer))
		response.Body.Close()
	}

	return time.Duration(sum / int64(times)), nil
}

type benchmarkResult struct {
	URL      string
	Duration time.Duration
}
