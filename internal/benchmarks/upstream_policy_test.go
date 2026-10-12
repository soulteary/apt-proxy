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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func benchmarkPolicy(t *testing.T, uri string, exception bool) *access.Upstream {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg := access.UpstreamConfig{Enabled: true, AllowedPorts: []int{number}}
	if exception {
		cfg.AllowCIDRs = []string{"127.0.0.1/32"}
	}
	p, err := access.NewUpstream(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestGuardedBenchmarkProbeFallbackAndIsolation(t *testing.T) {
	var head, get atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			head.Add(1)
			w.WriteHeader(405)
			return
		}
		get.Add(1)
		w.WriteHeader(200)
	}))
	defer source.Close()
	denied := NewEngineWithPolicy(benchmarkPolicy(t, source.URL, false))
	allowed := NewEngineWithPolicy(benchmarkPolicy(t, source.URL, true))
	if _, err := denied.Benchmark(context.Background(), source.URL, "/probe", 1); !errors.Is(err, access.ErrUpstreamDenied) {
		t.Fatalf("denied probe: %v", err)
	}
	if head.Load()+get.Load() != 0 {
		t.Fatal("denied probe contacted upstream")
	}
	if _, err := allowed.Benchmark(context.Background(), source.URL, "/probe", 1); err != nil {
		t.Fatal(err)
	}
	if head.Load() != 1 || get.Load() != 1 {
		t.Fatalf("HEAD=%d GET=%d", head.Load(), get.Load())
	}
	if _, err := denied.Benchmark(context.Background(), source.URL, "/probe", 1); !errors.Is(err, access.ErrUpstreamDenied) {
		t.Fatalf("another engine changed denial: %v", err)
	}
	if head.Load() != 1 || get.Load() != 1 {
		t.Fatal("policies leaked across engines")
	}
}
func TestGuardedBenchmarkRedirectAndAsync(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetHits.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	engine := NewEngineWithPolicy(benchmarkPolicy(t, source.URL, true))
	if _, err := engine.Benchmark(context.Background(), source.URL, "/probe", 1); !errors.Is(err, access.ErrUpstreamDenied) {
		t.Fatalf("redirect error=%v", err)
	}
	if targetHits.Load() != 0 {
		t.Fatal("unsafe redirect target contacted")
	}
	done := make(chan AsyncBenchmarkResult, 1)
	engine.GetTheFastestMirrorAsync(3, []string{source.URL}, "/probe", func(r AsyncBenchmarkResult) { done <- r })
	select {
	case result := <-done:
		if !errors.Is(result.Error, access.ErrUpstreamDenied) {
			t.Fatalf("async result=%+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("async benchmark did not stop")
	}
	if _, ok := engine.Cache().GetCachedResult(3); ok {
		t.Fatal("denied election cached")
	}
	if targetHits.Load() != 0 {
		t.Fatal("async redirect escaped policy")
	}
}
func TestGuardedGeoDiscoveryFallbackAndRegistry(t *testing.T) {
	p, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedHosts: []string{"mirror.example"}})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithPolicy(p)
	candidates := engine.MirrorCandidates(distro.NewBuiltinRegistry(), distro.TypeUbuntu)
	if len(candidates) == 0 {
		t.Fatal("geo denial lost builtin candidates")
	}
	// Every fallback candidate remains guarded, without reaching DNS or a network.
	if _, err := engine.GetTheFastestMirror(candidates, "/probe"); !errors.Is(err, access.ErrUpstreamDenied) {
		t.Fatalf("fallback escaped policy: %v", err)
	}
	reg := distro.NewRegistry()
	if err := reg.Register(&distro.RegisteredDistribution{ID: "ubuntu", Type: distro.TypeUbuntu, Mirrors: []distro.URLWithAlias{{URL: "https://mirror.example/ubuntu/", Scheme: "https"}}}); err != nil {
		t.Fatal(err)
	}
	configured := engine.MirrorCandidates(reg, distro.TypeUbuntu)
	if len(configured) != 1 || configured[0] != "https://mirror.example/ubuntu/" {
		t.Fatalf("configured mirrors=%v", configured)
	}
}

type geoDiscoveryFixture struct {
	calls   int
	uri     string
	bounded bool
}

func (f *geoDiscoveryFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	f.uri = r.URL.String()
	deadline, ok := r.Context().Deadline()
	f.bounded = ok && time.Until(deadline) <= 5*time.Second
	return nil, access.ErrUpstreamDenied
}
func TestGuardedGeoUsesEngineClientWithoutUnguardedRetry(t *testing.T) {
	p, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithPolicy(p)
	fixture := &geoDiscoveryFixture{}
	engine.client.Transport = fixture
	if candidates := engine.MirrorCandidates(distro.NewBuiltinRegistry(), distro.TypeUbuntu); len(candidates) == 0 {
		t.Fatal("discovery rejection lost fallback")
	}
	if fixture.calls != 1 || fixture.uri != distro.UbuntuGeoMirrorAPI || !fixture.bounded {
		t.Fatalf("geo fixture=%+v", fixture)
	}
}
