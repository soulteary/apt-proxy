package benchmarks_test

import (
	"errors"
	"testing"
	"time"

	Define "github.com/soulteary/apt-proxy/define"
	Benchmark "github.com/soulteary/apt-proxy/internal/benchmark"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

func TestBenchmark(t *testing.T) {
	const resourcePath = ""
	_, err := Benchmark.Benchmark(Define.UBUNTU_GEO_MIRROR_API, resourcePath, Benchmark.BENCHMARK_MAX_TRIES)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetTheFastestMirror(t *testing.T) {
	mirrors := Mirrors.GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_UBUNTU)
	_, err := Benchmark.GetTheFastestMirror(mirrors, Define.UBUNTU_BENCHMAKR_URL)
	if err != nil {
		t.Fatal(err)
	}

	// test empty list
	var emptyList []string
	_, err = Benchmark.GetTheFastestMirror(emptyList, "")
	if err == nil {
		t.Fatal("not cache an empty error")
	}
}

func TestReadResults(t *testing.T) {
	ch := make(chan Benchmark.Result)
	for i := 0; i < 5; i++ {
		go func(t int) {
			ch <- Benchmark.Result{"code", time.Duration(t)}
		}(i)
	}
	results, err := Benchmark.ReadResults(ch, 2)

	if len(results) == 0 {
		t.Fatal(errors.New("No results found: " + err.Error()))
	} else if err != nil {
		t.Fatalf("Error benchmarking mirrors: %s", err.Error())
	}
}
