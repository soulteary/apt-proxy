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

package proxy

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	logger "github.com/soulteary/logger-kit/v2"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// A reload publishes host patterns and rewriters together, so a request can
// never match a rule whose rewriter has not been built yet.
//
// The old order invalidated the host-pattern cache first and then rebuilt the
// rewriters synchronously, benchmarking mirrors as it went. For the length of
// that rebuild a request for a newly added distribution matched the new
// pattern, found no rewriter for its type, and was proxied with a relative
// URL -- a 502. This test holds the refresh open inside the benchmark and
// asserts that a request arriving in exactly that window is never served from
// a half-published snapshot.
//
// While the refresh is in flight the correct answer is the *old* snapshot: the
// new distribution is not live yet, so 404 is honest. What must not happen is
// a match without a rewrite.
func TestRefreshPublishesPatternsAndRewritersTogether(t *testing.T) {
	probeArrived := make(chan struct{})
	releaseProbe := make(chan struct{})
	var once sync.Once

	// The benchmark probe for the new distribution blocks here, which holds
	// RefreshMirrors open for as long as the test needs the window.
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(probeArrived) })
		<-releaseProbe
		w.WriteHeader(http.StatusOK)
	}))
	defer mirror.Close()

	// Built-ins only: the rewriter set this PackageStruct starts with has
	// nothing for the custom type, which is the whole point.
	reg := distro.NewBuiltinRegistry()
	st := newTestState()
	st.SetProxyMode(distro.TypeAllDistros)

	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: reg,
		CacheDir: t.TempDir(),
		Logger:   logger.Default(),
		Mode:     distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	// The reload a SIGHUP would perform: the registry gains a distribution
	// the running rewriters know nothing about.
	cfg := &distro.DistributionConfig{
		ID:           "deepin",
		Name:         "Deepin",
		Type:         customDistroType,
		URLPattern:   `/deepin/(.+)$`,
		BenchmarkURL: "dists/apricot/main/binary-amd64/Release",
		CacheRules: []distro.CacheRuleConfig{
			{Pattern: `InRelease$`, CacheControl: "max-age=3600", Rewrite: true},
		},
		Mirrors: distro.MirrorListConfig{Official: []string{mirror.URL + "/deepin/"}},
	}
	if err := reg.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}

	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		ps.RefreshMirrors()
	}()

	<-probeArrived // the refresh is now mid-flight, inside mirror election

	// The window. The invariant is not "no match" but "no match left
	// unrewritten": a matched rewrite rule whose URL still points at the host
	// the client addressed means the rewriter for that rule was not published
	// yet, which is precisely the 502 the old ordering produced in
	// production. Not matching at all is the honest answer here -- the
	// reloaded distribution is not live until its rewriters are.
	const clientHost = "community-packages.deepin.com"
	req := httptest.NewRequest(http.MethodGet,
		"http://"+clientHost+"/deepin/dists/apricot/InRelease", nil)
	switch rule := ps.handleExternalURLs(req); {
	case rule == nil:
		// Old snapshot still serving; the new distribution is not live yet.
	case rule.Rewrite && req.URL.Host == clientHost:
		t.Error("mid-refresh: matched a reloaded rule but the URL was never rewritten " +
			"to a mirror -- patterns went live ahead of their rewriters")
	}

	// The previous snapshot must keep serving correctly throughout, not merely
	// avoid breaking -- a built-in still rewrites to its configured mirror.
	req = httptest.NewRequest(http.MethodGet, "http://archive.ubuntu.com/ubuntu/dists/jammy/InRelease", nil)
	if rule := ps.handleExternalURLs(req); rule == nil {
		t.Error("mid-refresh: a built-in distribution stopped matching")
	} else if got, want := req.URL.Host, "mirrors.example.com"; got != want {
		t.Errorf("mid-refresh: built-in upstream host = %q, want %q", got, want)
	}

	close(releaseProbe)
	<-refreshDone

	// Once published, the new distribution routes for real.
	req = httptest.NewRequest(http.MethodGet,
		"http://"+clientHost+"/deepin/dists/apricot/InRelease", nil)
	if rule := ps.handleExternalURLs(req); rule == nil {
		t.Fatal("after refresh: the reloaded distribution does not match")
	}
	if got, want := req.URL.Host, mirror.Listener.Addr().String(); got != want {
		t.Errorf("after refresh: upstream host = %q, want %q (the elected mirror)", got, want)
	}
}
