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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// The reverse proxy hands upstream redirects to the package manager unfollowed,
// so a mirror that answers the probe with one must lose the election whatever
// the redirect leads to.
func TestProbeRejectsRedirects(t *testing.T) {
	var landingHits atomic.Int32
	landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		landingHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer landing.Close()
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer direct.Close()

	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, landing.URL+r.URL.Path, status)
			}))
			defer redirecting.Close()

			engine := NewEngine()
			_, err := engine.Benchmark(context.Background(), redirecting.URL, "probe", 1)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unexpected status %d", status)) {
				t.Fatalf("probe of a redirecting mirror: err = %v, want status %d reported", err, status)
			}
			fastest, err := engine.GetTheFastestMirror([]string{redirecting.URL, direct.URL}, "probe")
			if err != nil || fastest != direct.URL {
				t.Fatalf("GetTheFastestMirror() = %q, %v; want the mirror that answers directly", fastest, err)
			}
		})
	}
	if got := landingHits.Load(); got != 0 {
		t.Errorf("redirect target was requested %d times, want 0", got)
	}
}

// Geo discovery still follows redirects, over the transport the probes use.
func TestDiscoveryClientFollowsRedirects(t *testing.T) {
	landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "https://mirror.example/ubuntu/\n")
	}))
	defer landing.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, landing.URL, http.StatusFound)
	}))
	defer redirecting.Close()

	engine := NewEngine()
	if engine.discovery.Transport != engine.client.Transport {
		t.Fatal("discovery and probe clients use different transports")
	}
	resp, err := engine.discovery.Get(redirecting.URL)
	if err != nil {
		t.Fatalf("discovery through a redirect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || string(body) != "https://mirror.example/ubuntu/\n" {
		t.Fatalf("discovery through a redirect: status=%d body=%q err=%v", resp.StatusCode, body, err)
	}
}

// A 200 HTML page in place of the benchmark file is a landing page or a soft
// 404, not a repository.
func TestProbeRejectsHTMLResponses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		contentType string
		headStatus  int
		wantErr     bool
	}{
		{"html page", "text/html; charset=utf-8", http.StatusOK, true},
		{"html without parameters", "text/html", http.StatusOK, true},
		{"html in another case", "TEXT/HTML; Charset=UTF-8", http.StatusOK, true},
		{"html with a malformed parameter", "text/html; charset", http.StatusOK, true},
		{"html through the GET fallback", "text/html", http.StatusMethodNotAllowed, true},
		{"plain text", "text/plain", http.StatusOK, false},
		{"no content type", "", http.StatusOK, false},
		{"binary through the GET fallback", "application/octet-stream", http.StatusNotImplemented, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead && tc.headStatus != http.StatusOK {
					w.WriteHeader(tc.headStatus)
					return
				}
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			_, err := NewEngine().Benchmark(context.Background(), server.URL, "probe", 1)
			if tc.wantErr && (err == nil || !strings.Contains(err.Error(), "HTML")) {
				t.Fatalf("Benchmark() error = %v, want the HTML response rejected", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Benchmark() error = %v, want nil", err)
			}
		})
	}
}

// An election does not stop at the first few answers: every candidate gets
// all of its attempts, including those queued behind the concurrency limit.
func TestElectionProbesEveryCandidate(t *testing.T) {
	const candidates = MaxBenchmarkConcurrency + 4
	hits := make([]atomic.Int32, candidates)
	urls := make([]string, candidates)
	for i := range urls {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits[i].Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)
		urls[i] = server.URL
	}

	if _, err := NewEngine().GetTheFastestMirror(urls, "probe"); err != nil {
		t.Fatalf("GetTheFastestMirror() error = %v", err)
	}
	for i := range hits {
		if got := hits[i].Load(); got != BenchmarkMaxTries {
			t.Errorf("candidate %d was probed %d times, want %d", i, got, BenchmarkMaxTries)
		}
	}
}

func TestRankResults(t *testing.T) {
	const ms = time.Millisecond
	for _, tc := range []struct {
		name string
		in   Results
		want []string
	}{
		{
			name: "lower duration first",
			in:   Results{{"https://b.example/ubuntu/", 30 * ms}, {"https://a.example/ubuntu/", 20 * ms}, {"https://c.example/ubuntu/", 10 * ms}},
			want: []string{"https://c.example/ubuntu/", "https://a.example/ubuntu/", "https://b.example/ubuntu/"},
		},
		{
			name: "equal durations ordered by URL",
			in:   Results{{"https://c.example/ubuntu/", 10 * ms}, {"https://a.example/ubuntu/", 10 * ms}, {"https://b.example/ubuntu/", 10 * ms}},
			want: []string{"https://a.example/ubuntu/", "https://b.example/ubuntu/", "https://c.example/ubuntu/"},
		},
		{
			name: "https result replaces the faster http result of the same mirror",
			in:   Results{{"http://a.example/ubuntu/", 1 * ms}, {"https://a.example/ubuntu/", 9 * ms}, {"http://b.example/ubuntu/", 5 * ms}},
			want: []string{"http://b.example/ubuntu/", "https://a.example/ubuntu/"},
		},
		{
			name: "http result kept when https did not succeed",
			in:   Results{{"http://a.example/ubuntu/", 1 * ms}, {"https://b.example/ubuntu/", 9 * ms}},
			want: []string{"http://a.example/ubuntu/", "https://b.example/ubuntu/"},
		},
		{
			name: "another path on the same host is another mirror",
			in:   Results{{"http://a.example/ubuntu/", 1 * ms}, {"https://a.example/ubuntu-ports/", 9 * ms}},
			want: []string{"http://a.example/ubuntu/", "https://a.example/ubuntu-ports/"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, result := range rankResults(tc.in) {
				got = append(got, result.URL)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rankResults() = %v, want %v", got, tc.want)
			}
		})
	}
}

// stubMirror describes how a mirror answers probes on the fake clock of a
// synctest bubble.
type stubMirror struct {
	// latencies are the response times of successive attempts; the last one
	// repeats. A mirror without latencies never answers.
	latencies []time.Duration
	// status is the response status; zero means 200.
	status int
}

// stubTransport answers every probe from the stubMirror registered for its
// URL, without touching the network. It only makes sense inside synctest.Test,
// where the latencies elapse instantly and exactly.
type stubTransport struct {
	mirrors map[string]stubMirror

	mu       sync.Mutex
	attempts map[string]int
	inflight int
	peak     int
}

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	target := r.URL.String()
	mirror, ok := s.mirrors[target]
	if !ok {
		return nil, fmt.Errorf("unexpected probe of %s", target)
	}

	s.mu.Lock()
	attempt := s.attempts[target]
	s.attempts[target]++
	s.inflight++
	s.peak = max(s.peak, s.inflight)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()

	if len(mirror.latencies) == 0 {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	timer := time.NewTimer(mirror.latencies[min(attempt, len(mirror.latencies)-1)])
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}

	status := mirror.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
}

// stubEngine returns an engine whose probes are answered by the given mirrors.
func stubEngine(mirrors map[string]stubMirror) (*Engine, *stubTransport) {
	transport := &stubTransport{mirrors: mirrors, attempts: make(map[string]int)}
	engine := NewEngine()
	engine.client.Transport = transport
	return engine, transport
}

// answering returns a mirror whose every attempt takes the given time.
func answering(latency time.Duration) stubMirror {
	return stubMirror{latencies: []time.Duration{latency}}
}

// A mirror is scored by its fastest attempt, so the connection setup paid by
// the first attempt does not decide the election.
func TestElectionScoresMirrorsByTheirFastestAttempt(t *testing.T) {
	const (
		slowStart = "https://slow-start.example/ubuntu/"
		steady    = "https://steady.example/ubuntu/"
	)
	mirrors := map[string]stubMirror{
		// On average slower than steady, but faster once connected.
		slowStart: {latencies: []time.Duration{300 * time.Millisecond, 10 * time.Millisecond}},
		steady:    answering(50 * time.Millisecond),
	}
	synctest.Test(t, func(t *testing.T) {
		engine, _ := stubEngine(mirrors)
		score, err := engine.Benchmark(context.Background(), slowStart, "", BenchmarkMaxTries)
		if err != nil || score != 10*time.Millisecond {
			t.Fatalf("Benchmark() = %v, %v; want the fastest attempt (10ms)", score, err)
		}

		engine, _ = stubEngine(mirrors)
		fastest, err := engine.GetTheFastestMirror([]string{steady, slowStart}, "")
		if err != nil || fastest != slowStart {
			t.Fatalf("GetTheFastestMirror() = %q, %v; want %q", fastest, err, slowStart)
		}
	})
}

// When a mirror answers over both schemes, https is elected even though plain
// http answers sooner; http remains eligible where https fails.
func TestElectionPrefersHTTPSOfTheSameMirror(t *testing.T) {
	const (
		bothHTTP   = "http://both.example/ubuntu/"
		bothHTTPS  = "https://both.example/ubuntu/"
		plainHTTP  = "http://plain.example/ubuntu/"
		plainHTTPS = "https://plain.example/ubuntu/"
	)
	synctest.Test(t, func(t *testing.T) {
		engine, _ := stubEngine(map[string]stubMirror{
			bothHTTP:  answering(5 * time.Millisecond),
			bothHTTPS: answering(40 * time.Millisecond),
		})
		fastest, err := engine.GetTheFastestMirror([]string{bothHTTP, bothHTTPS}, "")
		if err != nil || fastest != bothHTTPS {
			t.Fatalf("GetTheFastestMirror() = %q, %v; want %q", fastest, err, bothHTTPS)
		}

		engine, _ = stubEngine(map[string]stubMirror{
			plainHTTP:  answering(5 * time.Millisecond),
			plainHTTPS: {latencies: []time.Duration{time.Millisecond}, status: http.StatusNotFound},
			bothHTTPS:  answering(40 * time.Millisecond),
		})
		fastest, err = engine.GetTheFastestMirror([]string{bothHTTPS, plainHTTPS, plainHTTP}, "")
		if err != nil || fastest != plainHTTP {
			t.Fatalf("GetTheFastestMirror() = %q, %v; want %q", fastest, err, plainHTTP)
		}
	})
}

// Equally fast mirrors are told apart by URL, not by list position or by which
// probe happened to finish first.
func TestElectionBreaksTiesByURL(t *testing.T) {
	const a, b, c = "https://a.example/ubuntu/", "https://b.example/ubuntu/", "https://c.example/ubuntu/"
	mirrors := map[string]stubMirror{
		a: answering(20 * time.Millisecond),
		b: answering(20 * time.Millisecond),
		c: answering(20 * time.Millisecond),
	}
	synctest.Test(t, func(t *testing.T) {
		for _, order := range [][]string{{a, b, c}, {c, b, a}, {b, c, a}, {c, a, b}} {
			engine, _ := stubEngine(mirrors)
			fastest, err := engine.GetTheFastestMirror(order, "")
			if err != nil || fastest != a {
				t.Errorf("GetTheFastestMirror(%v) = %q, %v; want %q", order, fastest, err, a)
			}
		}
	})
}

// Probing every candidate must not lift the concurrency limit: the mirrors
// beyond MaxBenchmarkConcurrency wait for a slot and are probed afterwards.
func TestElectionBoundsConcurrentProbes(t *testing.T) {
	mirrors := make(map[string]stubMirror)
	var candidates []string
	for i := range MaxBenchmarkConcurrency + 4 {
		candidate := fmt.Sprintf("https://mirror-%02d.example/ubuntu/", i)
		mirrors[candidate] = answering(time.Second)
		candidates = append(candidates, candidate)
	}
	synctest.Test(t, func(t *testing.T) {
		engine, transport := stubEngine(mirrors)
		if _, err := engine.GetTheFastestMirror(candidates, ""); err != nil {
			t.Fatalf("GetTheFastestMirror() error = %v", err)
		}
		if transport.peak != MaxBenchmarkConcurrency {
			t.Errorf("%d probes ran at once, want %d", transport.peak, MaxBenchmarkConcurrency)
		}
		for _, candidate := range candidates {
			if got := transport.attempts[candidate]; got != BenchmarkMaxTries {
				t.Errorf("%s was probed %d times, want %d", candidate, got, BenchmarkMaxTries)
			}
		}
	})
}

// A mirror that never answers is abandoned after BenchmarkProbeTimeout, which
// returns its slot to the mirrors queued behind it.
func TestUnresponsiveMirrorsReleaseTheirSlot(t *testing.T) {
	const healthy = "https://healthy.example/ubuntu/"
	mirrors := map[string]stubMirror{healthy: answering(20 * time.Millisecond)}
	// Enough silent mirrors to occupy every slot twice.
	var candidates []string
	for i := range 2 * MaxBenchmarkConcurrency {
		silent := fmt.Sprintf("https://silent-%02d.example/ubuntu/", i)
		mirrors[silent] = stubMirror{}
		candidates = append(candidates, silent)
	}
	candidates = append(candidates, healthy)

	synctest.Test(t, func(t *testing.T) {
		engine, _ := stubEngine(mirrors)
		start := time.Now()
		_, err := engine.Benchmark(context.Background(), candidates[0], "", BenchmarkMaxTries)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != BenchmarkProbeTimeout {
			t.Fatalf("probe of a silent mirror: err = %v after %v, want a timeout after %v", err, time.Since(start), BenchmarkProbeTimeout)
		}

		engine, _ = stubEngine(mirrors)
		start = time.Now()
		fastest, err := engine.GetTheFastestMirror(candidates, "")
		if err != nil || fastest != healthy {
			t.Fatalf("GetTheFastestMirror() = %q, %v; want %q", fastest, err, healthy)
		}
		if elapsed := time.Since(start); elapsed >= BenchmarkDetectTimeout {
			t.Errorf("election took %v: silent mirrors held their slots until the election deadline", elapsed)
		}
	})
}

// At BenchmarkDetectTimeout the election ends with the mirrors that have
// answered, without waiting for the rest.
func TestElectionDeadlineKeepsMirrorsThatAnswered(t *testing.T) {
	const quick = "https://quick.example/ubuntu/"
	// Every slow mirror answers just inside the probe timeout. One slot serves
	// a slow mirror in perMirror, so filling the slots for one round more than
	// fits before the deadline leaves the last round unfinished.
	slowAttempt := BenchmarkProbeTimeout - time.Second
	perMirror := slowAttempt * BenchmarkMaxTries
	rounds := int(BenchmarkDetectTimeout/perMirror) + 1

	mirrors := map[string]stubMirror{quick: answering(20 * time.Millisecond)}
	var candidates []string
	for i := range MaxBenchmarkConcurrency*rounds - 1 {
		slow := fmt.Sprintf("https://slow-%02d.example/ubuntu/", i)
		mirrors[slow] = answering(slowAttempt)
		candidates = append(candidates, slow)
	}
	candidates = append(candidates, quick)

	synctest.Test(t, func(t *testing.T) {
		engine, transport := stubEngine(mirrors)
		start := time.Now()
		fastest, err := engine.GetTheFastestMirror(candidates, "")
		if err != nil || fastest != quick {
			t.Fatalf("GetTheFastestMirror() = %q, %v; want %q", fastest, err, quick)
		}
		if elapsed := time.Since(start); elapsed != BenchmarkDetectTimeout {
			t.Errorf("election took %v, want it to end at the %v deadline", elapsed, BenchmarkDetectTimeout)
		}
		attempts := 0
		for _, count := range transport.attempts {
			attempts += count
		}
		if all := len(candidates) * BenchmarkMaxTries; attempts >= all {
			t.Errorf("%d of %d attempts were made: the deadline cut nothing short", attempts, all)
		}
	})
}

// A list too long to probe before BenchmarkDetectTimeout is not worked through
// to the end: the mirrors still queued at the deadline are skipped, and an
// election without any usable mirror reports the deadline.
func TestElectionDeadlineSkipsQueuedMirrors(t *testing.T) {
	// Every mirror fails its first attempt just inside the probe timeout, so
	// one slot gets through a mirror in slowAttempt and has started this many
	// when the deadline interrupts the last of them.
	slowAttempt := BenchmarkProbeTimeout - time.Second
	started := MaxBenchmarkConcurrency * (int(BenchmarkDetectTimeout/slowAttempt) + 1)

	mirrors := make(map[string]stubMirror)
	var candidates []string
	for i := range started + MaxBenchmarkConcurrency {
		failing := fmt.Sprintf("https://failing-%02d.example/ubuntu/", i)
		mirrors[failing] = stubMirror{latencies: []time.Duration{slowAttempt}, status: http.StatusServiceUnavailable}
		candidates = append(candidates, failing)
	}

	synctest.Test(t, func(t *testing.T) {
		engine, transport := stubEngine(mirrors)
		start := time.Now()
		fastest, err := engine.GetTheFastestMirror(candidates, "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("GetTheFastestMirror() = %q, %v; want the deadline reported", fastest, err)
		}
		if elapsed := time.Since(start); elapsed != BenchmarkDetectTimeout {
			t.Errorf("election took %v, want it to end at the %v deadline", elapsed, BenchmarkDetectTimeout)
		}
		if probed := len(transport.attempts); probed != started {
			t.Errorf("%d of %d mirrors were probed, want %d", probed, len(candidates), started)
		}
	})
}
