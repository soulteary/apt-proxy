package linux

import (
	"testing"
)

func TestBenchmark(t *testing.T) {
	_, err := benchmark(mirrorsUrl, "", benchmarkTimes)
	if err != nil {
		t.Fatal(err)
	}
}
