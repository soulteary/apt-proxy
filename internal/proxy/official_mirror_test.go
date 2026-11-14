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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/internal/state"
)

// The official address of every built-in distribution that has one, and the
// Debian security archive derived from Debian's.
const (
	officialUbuntu         = "http://archive.ubuntu.com/ubuntu/"
	officialUbuntuPorts    = "http://ports.ubuntu.com/ubuntu-ports/"
	officialDebian         = "https://deb.debian.org/debian/"
	officialDebianSecurity = "https://deb.debian.org/debian-security/"
	officialAlpine         = "https://dl-cdn.alpinelinux.org/alpine/"
)

// logEntries returns the JSON log lines written so far that carry message.
func logEntries(t *testing.T, logs *lockedBuffer, message string) []map[string]any {
	t.Helper()
	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if entry["message"] == message {
			found = append(found, entry)
		}
	}
	return found
}

// captureLogs sends the default logger to a buffer until the test ends. An
// election goroutine may still write to it while the test reads it.
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	previous := logger.Default()
	logger.SetDefault(logger.New(logger.Config{Level: logger.InfoLevel, Output: logs, Format: logger.FormatJSON}))
	t.Cleanup(func() { logger.SetDefault(previous) })
	return logs
}

// offlineAccess is an upstream policy that allows no host that exists. It
// refuses geo discovery and every probe before a connection is made, so an
// election under it fails without reaching the network.
func offlineAccess() *access.UpstreamConfig {
	return &access.UpstreamConfig{Enabled: true, AllowedHosts: []string{"unlisted.invalid"}}
}

// offlineEngine returns an engine under offlineAccess.
func offlineEngine(t *testing.T) *benchmarks.Engine {
	t.Helper()
	policy, err := access.NewUpstream(offlineAccess())
	if err != nil {
		t.Fatal(err)
	}
	return benchmarks.NewEngineWithPolicy(policy)
}

// unpinnedState returns a state in which no distribution has a pinned mirror.
func unpinnedState(election bool) *state.AppState {
	st := state.NewAppState()
	st.SetMirrorElection(election)
	return st
}

// mirrorOf returns the mirror and the security mirror published for mode.
func mirrorOf(rewriters *URLRewriters, mode int) (mirror, security string) {
	rw := rewriters.published(mode)
	if rw == nil {
		return "", ""
	}
	if rw.mirror != nil {
		mirror = rw.mirror.String()
	}
	if rw.securityMirror != nil {
		security = rw.securityMirror.String()
	}
	return mirror, security
}

// awaitLogEntries waits until count log lines carry message. An asynchronous
// election logs its outcome last, so waiting for that line also waits for its
// goroutine to be done with the logger before the test ends.
func awaitLogEntries(t *testing.T, logs *lockedBuffer, message string, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(logEntries(t, logs, message)) < count {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d %q log entries", len(logEntries(t, logs, message)), count, message)
		}
		time.Sleep(time.Millisecond)
	}
}

// customDistribution registers a distributions.yaml-style entry under /<id>/.
func customDistribution(t *testing.T, reg *distro.Registry, id string, distType int, official string, listed ...string) {
	t.Helper()
	if err := reg.LoadFromConfig(&distro.DistributionConfig{
		ID: id, Name: id, Type: distType,
		URLPattern: "/" + id + "/(.+)$", BenchmarkURL: "probe", OfficialURL: official,
		CacheRules: []distro.CacheRuleConfig{{Pattern: `.*`, Rewrite: true}},
		Mirrors:    distro.MirrorListConfig{Official: listed},
	}); err != nil {
		t.Fatalf("LoadFromConfig(%s): %v", id, err)
	}
}

// With election off, a distribution without a pinned mirror uses its official
// address at startup and on refresh. Nothing is probed, and an election result
// cached earlier is not used either.
func TestMirrorElectionOffUsesOfficialMirrors(t *testing.T) {
	firstCentos := mirrors.GetListedMirrorUrlsByMode(nil, distro.TypeCentOS)[0]
	want := map[int]string{
		distro.TypeUbuntu:      officialUbuntu,
		distro.TypeUbuntuPorts: officialUbuntuPorts,
		distro.TypeDebian:      officialDebian,
		distro.TypeAlpine:      officialAlpine,
		// CentOS has no official address: its first listed mirror stands in.
		distro.TypeCentOS: firstCentos,
	}

	for _, tc := range []struct {
		name  string
		build func(st *state.AppState, reg *distro.Registry, engine *benchmarks.Engine) (*URLRewriters, error)
	}{
		{"synchronous startup", func(st *state.AppState, reg *distro.Registry, engine *benchmarks.Engine) (*URLRewriters, error) {
			return CreateNewRewritersWithEngine(distro.TypeAllDistros, st, reg, engine), nil
		}},
		{"asynchronous startup", func(st *state.AppState, reg *distro.Registry, engine *benchmarks.Engine) (*URLRewriters, error) {
			return CreateNewRewritersAsyncWithEngine(distro.TypeAllDistros, st, reg, engine), nil
		}},
		{"refresh", func(st *state.AppState, reg *distro.Registry, engine *benchmarks.Engine) (*URLRewriters, error) {
			return buildRewriters(nil, distro.TypeAllDistros, st, reg, engine)
		}},
		{"refresh of an elected set", func(st *state.AppState, reg *distro.Registry, engine *benchmarks.Engine) (*URLRewriters, error) {
			// Every distribution is being served from a mirror that an
			// election chose. Election is off now, so none of them is kept.
			elected := &URLRewriters{}
			for mode := range want {
				elected.set(mode, &URLRewriter{mirror: mirrorURL("http://elected.example/archive/")})
			}
			return elected, RefreshRewritersWithEngine(elected, distro.TypeAllDistros, st, reg, engine)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			engine := offlineEngine(t)
			for mode := range want {
				engine.Cache().SetCachedResult(mode, "http://cached.example/archive/", time.Hour)
			}

			rewriters, err := tc.build(unpinnedState(false), newTestRegistry(), engine)
			if err != nil {
				t.Fatalf("building rewriters: %v", err)
			}
			for mode, wantMirror := range want {
				if got, _ := mirrorOf(rewriters, mode); got != wantMirror {
					t.Errorf("mirror of type %d = %q, want %q", mode, got, wantMirror)
				}
			}
			if _, security := mirrorOf(rewriters, distro.TypeDebian); security != officialDebianSecurity {
				t.Errorf("Debian security mirror = %q, want %q", security, officialDebianSecurity)
			}

			for _, message := range []string{
				"starting benchmark for mirrors", "async: starting background benchmark",
				"error finding fastest mirror", "using default mirror (async benchmark pending)",
			} {
				if found := logEntries(t, logs, message); len(found) != 0 {
					t.Errorf("election ran with election off: %d %q log entries", len(found), message)
				}
			}
			if found := logEntries(t, logs, "mirror election is off, using official mirror"); len(found) != 4 {
				t.Errorf("%d distributions logged their official mirror, want 4", len(found))
			}
			standIn := logEntries(t, logs, "mirror election is off and the distribution has no official mirror, using its first listed mirror")
			if len(standIn) != 1 || standIn[0]["distro"] != "CentOS" || standIn[0]["mirror"] != firstCentos || standIn[0]["level"] != "warn" {
				t.Errorf("warning about the mirror standing in for an official address = %v", standIn)
			}
		})
	}
}

// A pinned mirror is used as before, whatever the election setting is.
func TestPinnedMirrorIsUsedWithElectionOff(t *testing.T) {
	const pinned = "http://pinned.example/debian/"
	for _, election := range []bool{true, false} {
		st := unpinnedState(election)
		st.SetMirror(distro.TypeDebian, pinned)
		for name, rw := range map[string]*URLRewriter{
			"synchronous":  createRewriter(distro.TypeDebian, st, newTestRegistry(), offlineEngine(t)),
			"asynchronous": createRewriterAsync(distro.TypeDebian, st, newTestRegistry(), &URLRewriters{}, offlineEngine(t)),
		} {
			if rw == nil || rw.mirror == nil || rw.mirror.String() != pinned {
				t.Errorf("election=%v %s: mirror = %+v, want the pinned %s", election, name, rw, pinned)
			} else if rw.securityMirror == nil || rw.securityMirror.String() != "http://pinned.example/debian-security/" {
				t.Errorf("election=%v %s: security mirror = %v, want it derived from the pinned mirror", election, name, rw.securityMirror)
			}
		}
	}
}

// officialMirrorProxy builds a PackageStruct for every distribution on st and reg.
// Its elections run under offlineAccess, and its downloads end in a transport
// that records the URLs it is asked for in place of the guarded one.
func officialMirrorProxy(t *testing.T, st *state.AppState, reg *distro.Registry) (*PackageStruct, *[]string) {
	t.Helper()
	var fetched []string
	ps, err := NewPackageStruct(Options{
		State: st, Registry: reg, CacheDir: t.TempDir(), Mode: distro.TypeAllDistros,
		UpstreamAccess: offlineAccess(),
		TransportWrapper: func(http.RoundTripper) http.RoundTripper {
			return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				fetched = append(fetched, r.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{},
					Body: io.NopCloser(strings.NewReader("upstream")), Request: r,
				}, nil
			})
		},
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	return ps, &fetched
}

// With election off, requests are fetched from the official addresses. Debian
// security requests follow the Debian address to the same host, or go to the
// dedicated security mirror when one is configured.
func TestMirrorElectionOffServesRequestsFromOfficialMirrors(t *testing.T) {
	firstCentos := mirrors.GetListedMirrorUrlsByMode(nil, distro.TypeCentOS)[0]
	type route struct {
		request *http.Request
		want    string
	}
	get := func(path string) *http.Request { return httptest.NewRequest(http.MethodGet, path, nil) }
	routes := func(security string) []route {
		return []route{
			{get("/ubuntu/dists/noble/InRelease"), officialUbuntu + "dists/noble/InRelease"},
			{get("/ubuntu-ports/dists/noble/InRelease"), officialUbuntuPorts + "dists/noble/InRelease"},
			{get("/debian/dists/bookworm/InRelease"), officialDebian + "dists/bookworm/InRelease"},
			{get("/debian-security/dists/bookworm-security/InRelease"), security + "dists/bookworm-security/InRelease"},
			{proxyModeRequest(http.MethodGet, "security.debian.org", "/dists/trixie-security/InRelease"), security + "dists/trixie-security/InRelease"},
			{get("/alpine/v3.22/main/x86_64/APKINDEX.tar.gz"), officialAlpine + "v3.22/main/x86_64/APKINDEX.tar.gz"},
			{get("/centos/7/os/x86_64/repodata/repomd.xml"), firstCentos + "7/os/x86_64/repodata/repomd.xml"},
		}
	}

	for _, tc := range []struct {
		name     string
		pinned   string
		security string
	}{
		{name: "security derived from the official address", security: officialDebianSecurity},
		{name: "dedicated security mirror", pinned: "http://security.example/debian-security/", security: "http://security.example/debian-security/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := newTestRegistry()
			st := unpinnedState(false)
			st.SetProxyMode(distro.TypeAllDistros)
			st.SetDebianSecurityMirrorWithRegistry(tc.pinned, reg)
			ps, fetched := officialMirrorProxy(t, st, reg)

			// The same routes apply after a refresh.
			for _, step := range []string{"startup", "refresh"} {
				if step == "refresh" {
					if err := ps.RefreshMirrors(); err != nil {
						t.Fatalf("RefreshMirrors: %v", err)
					}
				}
				for _, route := range routes(tc.security) {
					*fetched = nil
					if status := serveRecorded(ps, route.request); status != http.StatusOK {
						t.Fatalf("%s: %s answered %d", step, route.want, status)
					}
					if len(*fetched) != 1 || (*fetched)[0] != route.want {
						t.Errorf("%s: fetched %v, want %s", step, *fetched, route.want)
					}
				}
			}
		})
	}
}

// Until its first asynchronous election has finished, a distribution is served
// from its official address rather than from the first entry of its candidate
// list. A distribution without an official address starts on that entry.
func TestAsyncElectionStartsOnOfficialMirror(t *testing.T) {
	logs := captureLogs(t)
	gate := make(chan struct{})
	candidate := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-gate }))
	t.Cleanup(candidate.Close)
	// Registered after the server, so it runs first and lets the server close.
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)

	// Nothing listens on port 1: this candidate fails without a timeout.
	const unreachable = "http://127.0.0.1:1/centos/"
	reg := newTestRegistry()
	soleCandidate(t, reg, distro.TypeDebian, candidate.URL+"/debian/")
	centos := soleCandidate(t, reg, distro.TypeCentOS, unreachable)
	centos.Mirrors = append(centos.Mirrors, distro.URLWithAlias{URL: candidate.URL + "/centos/", Scheme: "http"})
	st := newTestState()
	st.SetMirror(distro.TypeDebian, "") // Elect rather than pin the mirror.
	st.SetMirror(distro.TypeCentOS, "")

	rewriters := CreateNewRewritersAsyncWithEngine(distro.TypeAllDistros, st, reg, benchmarks.NewEngine())
	if mirror, security := mirrorOf(rewriters, distro.TypeDebian); mirror != officialDebian || security != officialDebianSecurity {
		t.Errorf("Debian before its election: mirror=%q security=%q, want the official %q and %q", mirror, security, officialDebian, officialDebianSecurity)
	}
	if mirror, _ := mirrorOf(rewriters, distro.TypeCentOS); mirror != unreachable {
		t.Errorf("CentOS before its election: mirror=%q, want its first candidate %q", mirror, unreachable)
	}

	release()
	awaitLogEntries(t, logs, "async benchmark completed, mirror updated", 2)
	if mirror, security := mirrorOf(rewriters, distro.TypeDebian); mirror != candidate.URL+"/debian/" || security != candidate.URL+"/debian-security/" {
		t.Errorf("Debian after its election: mirror=%q security=%q, want the elected mirror and the security mirror derived from it", mirror, security)
	}
	if mirror, _ := mirrorOf(rewriters, distro.TypeCentOS); mirror != candidate.URL+"/centos/" {
		t.Errorf("CentOS after its election: mirror=%q, want the elected mirror", mirror)
	}
}

// A failed asynchronous election leaves the distribution on its official
// address.
func TestAsyncElectionFailureKeepsOfficialMirror(t *testing.T) {
	logs := captureLogs(t)
	var down atomic.Bool
	down.Store(true)
	candidate := electionMirror(t, "candidate", &down)

	reg := newTestRegistry()
	soleCandidate(t, reg, distro.TypeAlpine, candidate.URL+"/alpine/")
	st := newTestState()
	st.SetMirror(distro.TypeAlpine, "") // Elect rather than pin the mirror.
	engine := benchmarks.NewEngine()
	rewriters := CreateNewRewritersAsyncWithEngine(distro.TypeAlpine, st, reg, engine)

	awaitLogEntries(t, logs, "async benchmark failed", 1)
	if mirror, _ := mirrorOf(rewriters, distro.TypeAlpine); mirror != officialAlpine {
		t.Errorf("mirror after the failed election = %q, want the official %q", mirror, officialAlpine)
	}
	if cached, ok := engine.Cache().GetCachedResult(distro.TypeAlpine); ok {
		t.Errorf("failed election cached %q", cached)
	}
}

// A synchronous election that finds no usable mirror falls back to the
// official address when there is no previous mirror to keep. A distribution
// without an official address is left without a mirror, as before.
func TestFailedElectionFallsBackToOfficialMirror(t *testing.T) {
	const customOfficial, customListed = "https://packages.example/custom/", "listed.example/custom/"
	for _, tc := range []struct {
		name         string
		mode         int
		official     string // official_url of the custom distribution
		security     string // dedicated Debian security mirror
		wantMirror   string
		wantSecurity string
	}{
		{name: "ubuntu", mode: distro.TypeUbuntu, wantMirror: officialUbuntu},
		{name: "ubuntu-ports", mode: distro.TypeUbuntuPorts, wantMirror: officialUbuntuPorts},
		{name: "debian", mode: distro.TypeDebian, wantMirror: officialDebian, wantSecurity: officialDebianSecurity},
		{
			name: "debian with a dedicated security mirror", mode: distro.TypeDebian,
			security:   "http://security.example/debian-security/",
			wantMirror: officialDebian, wantSecurity: "http://security.example/debian-security/",
		},
		{name: "alpine", mode: distro.TypeAlpine, wantMirror: officialAlpine},
		{name: "centos has no official address", mode: distro.TypeCentOS},
		{name: "custom distribution with official_url", mode: customDistroType, official: customOfficial, wantMirror: customOfficial},
		{name: "custom distribution without official_url", mode: customDistroType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			reg := newTestRegistry()
			customDistribution(t, reg, "custom", customDistroType, tc.official, customListed)
			st := unpinnedState(true)
			st.SetDebianSecurityMirrorWithRegistry(tc.security, reg)

			// The engine refuses every probe, so the election fails.
			rw := createRewriter(tc.mode, st, reg, offlineEngine(t))
			if rw == nil {
				t.Fatal("no rewriter")
			}
			rewriters := &URLRewriters{}
			rewriters.set(tc.mode, rw)
			if mirror, security := mirrorOf(rewriters, tc.mode); mirror != tc.wantMirror || security != tc.wantSecurity {
				t.Errorf("mirror=%q security=%q, want %q and %q", mirror, security, tc.wantMirror, tc.wantSecurity)
			}

			if failed := logEntries(t, logs, "error finding fastest mirror"); len(failed) != 1 {
				t.Fatalf("%d failed elections logged, want 1", len(failed))
			}
			fallback := logEntries(t, logs, "mirror election failed, using official mirror")
			if tc.wantMirror == "" {
				if len(fallback) != 0 {
					t.Errorf("fallback logged for a distribution without an official address: %v", fallback)
				}
				return
			}
			if len(fallback) != 1 || fallback[0]["level"] != "warn" || fallback[0]["mirror"] != tc.wantMirror {
				t.Errorf("fallback warning = %v, want one for %s", fallback, tc.wantMirror)
			}
		})
	}
}

// pathRecorder starts a mirror that answers with name and the requested path,
// and returns the paths it was asked for.
func pathRecorder(t *testing.T, name string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		_, _ = io.WriteString(w, name+" "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

// On refresh, a distribution whose election fails keeps its previous mirror,
// and only one without a previous mirror falls back to its official address.
// The official address is not probed: it is no candidate unless it is listed.
func TestRefreshPrefersPreviousMirrorOverOfficialMirror(t *testing.T) {
	official, officialPaths := pathRecorder(t, "official")
	var down atomic.Bool
	down.Store(true)
	listed := electionMirror(t, "listed", &down)

	reg := newTestRegistry()
	ps, err := NewPackageStruct(Options{State: newTestState(), Registry: reg, Mode: distro.TypeAllDistros})
	if err != nil {
		t.Fatal(err)
	}
	const pkg = "/deepin/pool/package.deb"

	// The reload adds a distribution whose only listed mirror is down. It has
	// no mirror to keep, so it starts on its official address.
	customDistribution(t, reg, "deepin", customDistroType, official.URL+"/deepin/", listed.URL+"/deepin/")
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("refresh with an official address to fall back to: %v", err)
	}
	if paths := officialPaths(); len(paths) != 0 {
		t.Errorf("the official address was probed: %v", paths)
	}
	if code, body := serve(ps, pkg); code != http.StatusOK || body != "official "+pkg {
		t.Errorf("after the failed election: status=%d body=%q, want the official mirror", code, body)
	}

	// Its listed mirror answers again and is elected.
	down.Store(false)
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("refresh with a healthy mirror: %v", err)
	}
	if code, body := serve(ps, pkg); code != http.StatusOK || body != "listed "+pkg {
		t.Errorf("after the election: status=%d body=%q, want the listed mirror", code, body)
	}

	// The elected mirror fails a later election and is kept all the same.
	down.Store(true)
	logs := captureLogs(t)
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("refresh with a previous mirror to keep: %v", err)
	}
	if kept := logEntries(t, logs, "mirror election failed, keeping previous mirror"); len(kept) != 1 || kept[0]["mirror"] != listed.URL+"/deepin/" {
		t.Errorf("previous mirror warning = %v", kept)
	}
	if fallback := logEntries(t, logs, "mirror election failed, using official mirror"); len(fallback) != 0 {
		t.Errorf("official address replaced a previous mirror: %v", fallback)
	}
	down.Store(false)
	if code, body := serve(ps, pkg); code != http.StatusOK || body != "listed "+pkg {
		t.Errorf("after the second failed election: status=%d body=%q, want the previous mirror", code, body)
	}
	if paths := officialPaths(); len(paths) != 1 || paths[0] != pkg {
		t.Errorf("requests to the official address = %v, want the one package request", paths)
	}
}

// With election off, a distributions.yaml entry uses its official_url, or
// without one its first listed mirror. No listed mirror is probed, and turning
// election off takes effect on the next refresh.
func TestMirrorElectionOffForCustomDistributions(t *testing.T) {
	official, officialPaths := pathRecorder(t, "official")
	first, firstPaths := pathRecorder(t, "first")
	second, secondPaths := pathRecorder(t, "second")

	reg := newTestRegistry()
	customDistribution(t, reg, "with-official", 61, official.URL+"/with-official/", second.URL+"/with-official/")
	customDistribution(t, reg, "listed-only", 62, "", first.URL+"/listed-only/", second.URL+"/listed-only/")
	st := newTestState()
	ps, err := NewPackageStruct(Options{State: st, Registry: reg, Mode: distro.TypeAllDistros})
	if err != nil {
		t.Fatal(err)
	}
	// Election is on: each distribution elected among its listed mirrors.
	if code, body := serve(ps, "/with-official/pool/a.deb"); code != http.StatusOK || body != "second /with-official/pool/a.deb" {
		t.Fatalf("elected route: status=%d body=%q", code, body)
	}
	probed := len(firstPaths()) + len(secondPaths())

	st.SetMirrorElection(false)
	logs := captureLogs(t)
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("RefreshMirrors: %v", err)
	}
	if got := len(firstPaths()) + len(secondPaths()); got != probed {
		t.Errorf("refresh with election off sent %d requests to the listed mirrors", got-probed)
	}
	if paths := officialPaths(); len(paths) != 0 {
		t.Errorf("the official address was probed: %v", paths)
	}
	// The mirror elected before is not kept: the official address replaces it.
	if code, body := serve(ps, "/with-official/pool/a.deb"); code != http.StatusOK || body != "official /with-official/pool/a.deb" {
		t.Errorf("official_url route: status=%d body=%q", code, body)
	}
	if code, body := serve(ps, "/listed-only/pool/b.deb"); code != http.StatusOK || body != "first /listed-only/pool/b.deb" {
		t.Errorf("first listed mirror route: status=%d body=%q", code, body)
	}
	standIn := logEntries(t, logs, "mirror election is off and the distribution has no official mirror, using its first listed mirror")
	if len(standIn) != 1 || standIn[0]["distro"] != "listed-only" || standIn[0]["mirror"] != first.URL+"/listed-only/" {
		t.Errorf("warning about the first listed mirror = %v", standIn)
	}
}

// With election off, a distribution that has neither an official address nor
// a listed mirror gets no mirror, and a refresh reports it instead of keeping
// a mirror elected earlier.
func TestMirrorElectionOffWithoutAnyMirror(t *testing.T) {
	logs := captureLogs(t)
	reg := newTestRegistry()
	customDistribution(t, reg, "bare", customDistroType, "")
	st := newTestState()
	st.SetMirrorElection(false)

	if rw := createRewriter(customDistroType, st, reg, offlineEngine(t)); rw == nil || rw.mirror != nil {
		t.Errorf("rewriter = %+v, want one without a mirror", rw)
	}
	if rw := createRewriterAsync(customDistroType, st, reg, &URLRewriters{}, offlineEngine(t)); rw == nil || rw.mirror != nil {
		t.Errorf("asynchronous rewriter = %+v, want one without a mirror", rw)
	}
	previous := &URLRewriters{}
	previous.set(customDistroType, &URLRewriter{mirror: mirrorURL("http://elected.example/bare/")})
	if _, err := buildRewriters(previous, distro.TypeAllDistros, st, reg, offlineEngine(t)); err == nil || !strings.Contains(err.Error(), "no usable mirror") {
		t.Errorf("refresh = %v, want it to report the distribution without a mirror", err)
	}
	warned := logEntries(t, logs, "mirror election is off and the distribution has no official or listed mirror")
	if len(warned) != 3 || warned[0]["distro"] != "bare" {
		t.Errorf("warnings = %v, want one per construction", warned)
	}
}

func TestMirrorURLRequiresHost(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://mirror.example/debian/": true,
		"http://127.0.0.1:8080":          true,
		"":                               false,
		"mirror.example/debian/":         false,
		"/debian/":                       false,
		"https://mirror.example/%zz":     false,
	} {
		if got := mirrorURL(raw); (got != nil) != want {
			t.Errorf("mirrorURL(%q) = %v, want a mirror: %v", raw, got, want)
		}
	}
}
