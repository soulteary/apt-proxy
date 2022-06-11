package linux

import (
	"testing"
)

func TestBenchmark(t *testing.T) {
	_, err := benchmark(UBUNTU_MIRROR_URLS, "", benchmarkTimes)
	if err != nil {
		t.Fatal(err)
	}
}
