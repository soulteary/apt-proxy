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
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/passthrough"
)

// proxmoxSetType is the type of the custom distribution in these tests.
const proxmoxSetType = 100

// probedMirror starts a local mirror for a custom distribution and counts the
// requests it receives. A custom distribution has no pinned mirror, so
// building its rewriter elects one with benchmark probes; the count shows
// whether that election ran. The proxied requests of these tests go to a
// recording transport instead and are not counted.
func probedMirror(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var probes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, &probes
}

// addCustomDistro registers a distribution the way distributions.yaml does.
func addCustomDistro(t *testing.T, reg *distro.Registry, cfg distro.DistributionConfig) {
	t.Helper()
	if cfg.Name == "" {
		cfg.Name = cfg.ID
	}
	if cfg.BenchmarkURL == "" {
		cfg.BenchmarkURL = "dists/stable/Release"
	}
	if cfg.CacheRules == nil {
		cfg.CacheRules = []distro.CacheRuleConfig{{Pattern: ".*", CacheControl: "max-age=3600", Rewrite: true}}
	}
	if err := reg.LoadFromConfig(&cfg); err != nil {
		t.Fatalf("LoadFromConfig(%s): %v", cfg.ID, err)
	}
}

// registryWithProxmox returns the built-in distributions plus "proxmox",
// served under /proxmox/ from the path /pve/ of mirror.
func registryWithProxmox(t *testing.T, mirror string) *distro.Registry {
	t.Helper()
	reg := distro.NewBuiltinRegistry()
	addCustomDistro(t, reg, distro.DistributionConfig{
		ID: "proxmox", Type: proxmoxSetType, URLPattern: `/proxmox/(.+)$`,
		Mirrors: distro.MirrorListConfig{Official: []string{mirror + "/pve/"}},
	})
	return reg
}

// resolveMode resolves mode values the way the server does at startup.
func resolveMode(t *testing.T, reg *distro.Registry, ids ...string) distro.ModeSet {
	t.Helper()
	mode, err := distro.ResolveModeSet(ids, reg)
	if err != nil {
		t.Fatalf("ResolveModeSet(%q): %v", ids, err)
	}
	return mode
}

// recordingProxy builds a PackageStruct on the mirrors of newTestState with a
// transport that records the upstream request instead of dialling. Every
// built-in distribution has a pinned mirror in that state, whether the mode
// enables it or not.
func recordingProxy(t *testing.T, opts Options) (*PackageStruct, *recordedRequest) {
	t.Helper()
	seen := &recordedRequest{}
	opts.State = newTestState()
	opts.CacheDir = t.TempDir()
	opts.Logger = logger.Default()
	opts.TransportOverride = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		seen.count++
		seen.host, seen.path = r.URL.Host, r.URL.EscapedPath()
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader("upstream")), Request: r,
		}, nil
	})
	ps, err := NewPackageStruct(opts)
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	return ps, seen
}

// requestForms returns the two forms a client request for path arrives in.
// The host is one no distribution or mirror uses.
func requestForms(path string) map[string]*http.Request {
	const named = "internal.example"
	return map[string]*http.Request{
		// A proxy client: the request line carries the authority.
		"absolute form": httptest.NewRequest(http.MethodGet, "http://"+named+path, nil),
		// A redirected or direct client: only Host carries it.
		"origin form": proxyModeRequest(http.MethodGet, named, path),
	}
}

// routedIDs lists the distributions that take part in request matching.
func routedIDs(ps *PackageStruct) []string {
	var ids []string
	for _, entry := range ps.currentRouting().patterns {
		ids = append(ids, entry.repositoryID)
	}
	sort.Strings(ids)
	return ids
}

// A distribution is served when the mode enables it and is otherwise left
// out completely: no route, no rewriter and no mirror election, even with a
// mirror pinned for it.
func TestModeSetServesExactlyTheEnabledDistributions(t *testing.T) {
	// One path of every distribution, and the upstream path at its mirror.
	paths := []struct {
		id, path, upstreamPath string
		distType               int
	}{
		{"ubuntu", "/ubuntu/dists/x/InRelease", "/ubuntu/dists/x/InRelease", distro.TypeUbuntu},
		{"ubuntu-ports", "/ubuntu-ports/dists/x/InRelease", "/ubuntu-ports/dists/x/InRelease", distro.TypeUbuntuPorts},
		{"debian", "/debian/dists/x/InRelease", "/debian/dists/x/InRelease", distro.TypeDebian},
		{"centos", "/centos/9-stream/BaseOS/x86_64/os/repodata/repomd.xml", "/centos/9-stream/BaseOS/x86_64/os/repodata/repomd.xml", distro.TypeCentOS},
		{"alpine", "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz", "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz", distro.TypeAlpine},
		{"proxmox", "/proxmox/dists/trixie/InRelease", "/pve/dists/trixie/InRelease", proxmoxSetType},
	}
	everything := []string{"alpine", "centos", "debian", "proxmox", "ubuntu", "ubuntu-ports"}

	for _, tc := range []struct {
		name string
		// configure selects the mode on opts, given the registry.
		configure func(t *testing.T, opts *Options, reg *distro.Registry)
		// enabled lists the IDs that must be served, sorted.
		enabled []string
	}{
		{
			name: "debian and a custom distribution",
			configure: func(t *testing.T, opts *Options, reg *distro.Registry) {
				opts.ModeSet = resolveMode(t, reg, "debian", "proxmox")
			},
			enabled: []string{"debian", "proxmox"},
		},
		{
			name: "two built-ins",
			configure: func(t *testing.T, opts *Options, reg *distro.Registry) {
				opts.ModeSet = resolveMode(t, reg, "ubuntu", "alpine")
			},
			enabled: []string{"alpine", "ubuntu"},
		},
		{
			name: "one built-in by ID",
			configure: func(t *testing.T, opts *Options, reg *distro.Registry) {
				opts.ModeSet = resolveMode(t, reg, "debian")
			},
			enabled: []string{"debian"},
		},
		{
			name:      "one built-in by the single type",
			configure: func(_ *testing.T, opts *Options, _ *distro.Registry) { opts.Mode = distro.TypeDebian },
			enabled:   []string{"debian"},
		},
		{
			name: "a custom distribution alone",
			configure: func(t *testing.T, opts *Options, reg *distro.Registry) {
				opts.ModeSet = resolveMode(t, reg, "proxmox")
			},
			enabled: []string{"proxmox"},
		},
		{
			name: "the set takes precedence over the single type",
			configure: func(t *testing.T, opts *Options, reg *distro.Registry) {
				opts.Mode = distro.TypeUbuntu
				opts.ModeSet = resolveMode(t, reg, "centos")
			},
			enabled: []string{"centos"},
		},
		{
			name: "all",
			configure: func(t *testing.T, opts *Options, reg *distro.Registry) {
				opts.ModeSet = resolveMode(t, reg, "all")
			},
			enabled: everything,
		},
		{
			name:      "nothing configured",
			configure: func(*testing.T, *Options, *distro.Registry) {},
			enabled:   everything,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mirror, probes := probedMirror(t)
			reg := registryWithProxmox(t, mirror.URL)
			opts := Options{Registry: reg}
			tc.configure(t, &opts, reg)
			ps, seen := recordingProxy(t, opts)

			enabled := make(map[string]bool, len(tc.enabled))
			for _, id := range tc.enabled {
				enabled[id] = true
			}

			if got := routedIDs(ps); !reflect.DeepEqual(got, tc.enabled) {
				t.Errorf("distributions taking part in matching = %v, want %v", got, tc.enabled)
			}
			for _, p := range paths {
				if got := ps.currentRouting().rewriters.published(p.distType) != nil; got != enabled[p.id] {
					t.Errorf("%s has a rewriter = %v, want %v", p.id, got, enabled[p.id])
				}
			}
			// The custom distribution elects its mirror with probes. None may
			// be sent while it is not enabled.
			if elected := probes.Load() > 0; elected != enabled["proxmox"] {
				t.Errorf("mirror election of proxmox ran = %v (%d probes), want %v", elected, probes.Load(), enabled["proxmox"])
			}

			for _, p := range paths {
				wantHost := testMirrorHost
				if p.id == "proxmox" {
					wantHost = strings.TrimPrefix(mirror.URL, "http://")
				}
				for form, request := range requestForms(p.path) {
					t.Run(p.id+"/"+form, func(t *testing.T) {
						*seen = recordedRequest{}
						rec := httptest.NewRecorder()
						ps.ServeHTTP(rec, request)

						if !enabled[p.id] {
							if rec.Code != http.StatusNotFound || seen.count != 0 {
								t.Fatalf("status = %d with %d upstream request(s) to %q, want 404 and none", rec.Code, seen.count, seen.host)
							}
							return
						}
						if rec.Code != http.StatusOK || seen.count != 1 {
							t.Fatalf("status = %d with %d upstream request(s), want 200 and one", rec.Code, seen.count)
						}
						if seen.host != wantHost || seen.path != p.upstreamPath {
							t.Fatalf("upstream request went to %s%s, want %s%s", seen.host, seen.path, wantHost, p.upstreamPath)
						}
					})
				}
			}
		})
	}
}

// The asynchronous constructor, which the daemon uses, builds the same set.
func TestAsyncRewritersCoverOnlyTheEnabledDistributions(t *testing.T) {
	st := newTestState()
	reg := newTestRegistry()
	rewriters := CreateNewRewritersAsyncWithEngine(distro.ModeOf(distro.TypeDebian, distro.TypeAlpine), st, reg, nil)
	for distType, want := range map[int]bool{
		distro.TypeUbuntu: false, distro.TypeUbuntuPorts: false, distro.TypeDebian: true,
		distro.TypeCentOS: false, distro.TypeAlpine: true,
	} {
		if got := rewriters.published(distType) != nil; got != want {
			t.Errorf("type %d has a rewriter = %v, want %v", distType, got, want)
		}
	}
}

// A distribution outside the mode must not claim requests. Its entry used to
// stay in the match list, where it answered 404 for every path it matched,
// including the paths an enabled distribution with an overlapping pattern
// serves.
func TestDisabledDistributionDoesNotShadowAnEnabledOne(t *testing.T) {
	t.Run("custom distribution on a built-in path", func(t *testing.T) {
		mirror, _ := probedMirror(t)
		reg := distro.NewBuiltinRegistry()
		// The built-in Debian entry matches /debian/ too and is tried first.
		addCustomDistro(t, reg, distro.DistributionConfig{
			ID: "debian-fork", Type: proxmoxSetType, URLPattern: `/debian/(.+)$`,
			Mirrors: distro.MirrorListConfig{Official: []string{mirror.URL + "/fork/"}},
		})
		ps, seen := recordingProxy(t, Options{Registry: reg, ModeSet: resolveMode(t, reg, "debian-fork")})

		for form, request := range requestForms("/debian/dists/x/InRelease") {
			t.Run(form, func(t *testing.T) {
				*seen = recordedRequest{}
				rec := httptest.NewRecorder()
				ps.ServeHTTP(rec, request)
				if rec.Code != http.StatusOK || seen.count != 1 {
					t.Fatalf("status = %d with %d upstream request(s), want 200 and one", rec.Code, seen.count)
				}
				if want := strings.TrimPrefix(mirror.URL, "http://"); seen.host != want || seen.path != "/fork/dists/x/InRelease" {
					t.Fatalf("upstream request went to %s%s, want %s/fork/dists/x/InRelease", seen.host, seen.path, want)
				}
			})
		}
	})

	t.Run("host pattern of a custom distribution", func(t *testing.T) {
		mirror, probes := probedMirror(t)
		reg := distro.NewBuiltinRegistry()
		// With this entry enabled, its host and path together take priority
		// over the Debian route. It is not enabled here.
		addCustomDistro(t, reg, distro.DistributionConfig{
			ID: "pve", Type: proxmoxSetType, URLPattern: `/debian/(.+)$`,
			HostPattern: `^download\.proxmox\.com(:\d+)?$`,
			Mirrors:     distro.MirrorListConfig{Official: []string{mirror.URL + "/debian/"}},
		})
		ps, seen := recordingProxy(t, Options{Registry: reg, ModeSet: resolveMode(t, reg, "debian")})

		const path = "/debian/dists/x/InRelease"
		for form, request := range map[string]*http.Request{
			"absolute form": absoluteRequest("download.proxmox.com", path),
			"origin form":   proxyModeRequest(http.MethodGet, "download.proxmox.com", path),
		} {
			t.Run(form, func(t *testing.T) {
				*seen = recordedRequest{}
				rec := httptest.NewRecorder()
				ps.ServeHTTP(rec, request)
				if rec.Code != http.StatusOK || seen.count != 1 || seen.host != testMirrorHost || seen.path != path {
					t.Fatalf("status = %d, %d upstream request(s) to %s%s; want 200 and one to %s%s",
						rec.Code, seen.count, seen.host, seen.path, testMirrorHost, path)
				}
			})
		}
		if probes.Load() != 0 {
			t.Errorf("the disabled distribution's mirror received %d probe(s)", probes.Load())
		}
	})
}

// An origin the operator allowlisted is fetched from that origin when the
// path looks like one of a distribution the mode does not enable. That holds
// also for an origin that is a listed mirror of the disabled distribution:
// apt-proxy does not manage that distribution in this mode, so nothing but
// the allowlist entry applies to the request.
func TestAllowlistedOriginWithPathOfDisabledDistribution(t *testing.T) {
	ubuntu, ok := newTestRegistry().GetByType(distro.TypeUbuntu)
	if !ok || len(ubuntu.Mirrors) == 0 {
		t.Fatal("the built-in registry lists no Ubuntu mirror")
	}
	var listedMirror string
	for host := range mirrorHostSet(ubuntu.Mirrors[:1]) {
		listedMirror = host
	}
	if listedMirror == "" {
		t.Fatalf("unusable listed mirror %q", ubuntu.Mirrors[0].URL)
	}

	for _, tc := range []struct{ name, origin, path string }{
		{"third-party origin", "packages.microsoft.com", "/ubuntu/22.04/prod/dists/jammy/InRelease"},
		{"listed mirror of the disabled distribution", listedMirror, "/ubuntu/dists/jammy/InRelease"},
		{"selected mirror of the disabled distribution", testMirrorHost, "/ubuntu/dists/jammy/InRelease"},
	} {
		for form, request := range map[string]*http.Request{
			"absolute form": absoluteRequest(tc.origin, tc.path),
			"origin form":   proxyModeRequest(http.MethodGet, tc.origin, tc.path),
		} {
			t.Run(tc.name+"/"+form, func(t *testing.T) {
				list, err := passthrough.Parse([]string{tc.origin})
				if err != nil {
					t.Fatalf("passthrough.Parse: %v", err)
				}
				ps, seen := recordingProxy(t, Options{
					Registry: newTestRegistry(), Mode: distro.TypeDebian, Passthrough: list,
				})

				rec := httptest.NewRecorder()
				ps.ServeHTTP(rec, request)
				if rec.Code != http.StatusOK || seen.count != 1 {
					t.Fatalf("status = %d with %d upstream request(s), want 200 and one", rec.Code, seen.count)
				}
				if seen.host != tc.origin || seen.path != tc.path {
					t.Fatalf("upstream request went to %s%s, want the allowlisted origin %s%s", seen.host, seen.path, tc.origin, tc.path)
				}

				// The enabled distribution keeps its own routing next to it.
				*seen = recordedRequest{}
				if status := serveRecorded(ps, absoluteRequest("internal.example", "/debian/dists/x/InRelease")); status != http.StatusOK {
					t.Fatalf("Debian request: status = %d, want 200", status)
				}
				if seen.host != testMirrorHost {
					t.Fatalf("Debian request went to %s, want the selected mirror", seen.host)
				}
			})
		}
	}
}

// A refresh keeps the enabled set. After a reload of distributions.yaml the
// caller passes the set again, because an enabled ID can have another type.
func TestRefreshMirrorsKeepsAndReplacesTheEnabledSet(t *testing.T) {
	mirror, _ := probedMirror(t)
	reg := registryWithProxmox(t, mirror.URL)
	ps, seen := recordingProxy(t, Options{Registry: reg, ModeSet: resolveMode(t, reg, "debian", "proxmox")})

	served := func(path string) bool {
		t.Helper()
		*seen = recordedRequest{}
		status := serveRecorded(ps, absoluteRequest("internal.example", path))
		if status != http.StatusOK && status != http.StatusNotFound {
			t.Fatalf("%s: status = %d", path, status)
		}
		return status == http.StatusOK
	}
	check := func(when string, want map[string]bool) {
		t.Helper()
		for path, wantServed := range want {
			if got := served(path); got != wantServed {
				t.Errorf("%s: %s served = %v, want %v", when, path, got, wantServed)
			}
		}
	}
	const debianPath, ubuntuPath, proxmoxPath = "/debian/dists/x/InRelease", "/ubuntu/dists/x/InRelease", "/proxmox/dists/x/InRelease"

	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("RefreshMirrors: %v", err)
	}
	check("after a plain refresh", map[string]bool{debianPath: true, proxmoxPath: true, ubuntuPath: false})
	if ps.currentRouting().rewriters.published(distro.TypeUbuntu) != nil {
		t.Error("a refresh built a rewriter for a distribution outside the mode")
	}

	// The reload gives proxmox another type. The set resolved at startup
	// names a type that no longer exists.
	if err := reg.Unregister("proxmox"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	addCustomDistro(t, reg, distro.DistributionConfig{
		ID: "proxmox", Type: proxmoxSetType + 1, URLPattern: `/proxmox/(.+)$`,
		Mirrors: distro.MirrorListConfig{Official: []string{mirror.URL + "/pve/"}},
	})
	if err := ps.RefreshMirrors(); err == nil {
		t.Fatal("a refresh with an enabled type the registry no longer defines succeeded")
	}
	check("after the failed refresh", map[string]bool{debianPath: true, proxmoxPath: true, ubuntuPath: false})

	if err := ps.RefreshMirrorsWithMode(resolveMode(t, reg, "debian", "proxmox")); err != nil {
		t.Fatalf("RefreshMirrorsWithMode: %v", err)
	}
	check("after the refresh with the resolved set", map[string]bool{debianPath: true, proxmoxPath: true, ubuntuPath: false})
	rewriters := ps.currentRouting().rewriters
	if rewriters.published(proxmoxSetType+1) == nil || rewriters.published(proxmoxSetType) != nil {
		t.Error("the rewriters do not follow the new type of proxmox")
	}

	// A set the registry cannot satisfy changes nothing, including the set
	// that the next plain refresh uses.
	if err := ps.RefreshMirrorsWithMode(distro.ModeOf(distro.TypeUbuntu, 9999)); err == nil {
		t.Fatal("a refresh with an unknown type succeeded")
	}
	check("after the refused set", map[string]bool{debianPath: true, proxmoxPath: true, ubuntuPath: false})
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("RefreshMirrors after the refused set: %v", err)
	}
	check("after the following plain refresh", map[string]bool{debianPath: true, proxmoxPath: true, ubuntuPath: false})

	// The set can also be widened and narrowed.
	if err := ps.RefreshMirrorsWithMode(resolveMode(t, reg, "ubuntu")); err != nil {
		t.Fatalf("RefreshMirrorsWithMode: %v", err)
	}
	check("after narrowing to ubuntu", map[string]bool{debianPath: false, proxmoxPath: false, ubuntuPath: true})
	if err := ps.RefreshMirrorsWithMode(distro.ModeSet{}); err != nil {
		t.Fatalf("RefreshMirrorsWithMode: %v", err)
	}
	check("after widening to all", map[string]bool{debianPath: true, proxmoxPath: true, ubuntuPath: true})
}

// Without usable registry entries the compile-time patterns apply. They are
// limited to the enabled distributions as well.
func TestFallbackPatternsFollowTheMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode distro.ModeSet
		want []string
	}{
		{"all", distro.ModeSet{}, []string{"alpine", "centos", "debian", "ubuntu", "ubuntu-ports"}},
		{"two built-ins", distro.ModeOf(distro.TypeUbuntu, distro.TypeDebian), []string{"debian", "ubuntu"}},
		{"a type without compile-time patterns", distro.ModeOf(proxmoxSetType), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps, seen := recordingProxy(t, Options{Registry: distro.NewRegistry(), ModeSet: tc.mode})
			if got := routedIDs(ps); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fallback patterns = %v, want %v", got, tc.want)
			}
			wantUbuntu := tc.mode.Has(distro.TypeUbuntu)
			status := serveRecorded(ps, absoluteRequest("internal.example", "/ubuntu/dists/x/InRelease"))
			if served := status == http.StatusOK && seen.count == 1; served != wantUbuntu {
				t.Fatalf("Ubuntu request: status = %d with %d upstream request(s), want served = %v", status, seen.count, wantUbuntu)
			}
		})
	}
}

// Rules lists the caching rules of the enabled distributions.
func TestRulesFollowTheMode(t *testing.T) {
	mirror, _ := probedMirror(t)
	reg := registryWithProxmox(t, mirror.URL)

	set, _ := recordingProxy(t, Options{Registry: reg, ModeSet: resolveMode(t, reg, "proxmox", "debian")})
	want := append(append([]distro.Rule(nil), GetRewriteRulesByMode(reg, distro.TypeDebian)...), GetRewriteRulesByMode(reg, proxmoxSetType)...)
	if !reflect.DeepEqual(set.Rules, want) {
		t.Errorf("Rules of the set hold %d rules, want the %d of Debian followed by proxmox", len(set.Rules), len(want))
	}

	all, _ := recordingProxy(t, Options{Registry: reg})
	if !reflect.DeepEqual(all.Rules, GetRewriteRulesByMode(reg, distro.TypeAllDistros)) {
		t.Errorf("Rules of the default mode hold %d rules, want the aggregate as before", len(all.Rules))
	}
}

// distro.ModeSet.Types and this package both order the built-in types. Host
// patterns are walked in distroModesOrder and rewriters are built in the
// order of Types; the two must not drift apart.
func TestBuiltinOrderMatchesModeSet(t *testing.T) {
	if got := (distro.ModeSet{}).Types(nil); !reflect.DeepEqual(got, distroModesOrder) {
		t.Fatalf("ModeSet.Types = %v, distroModesOrder = %v", got, distroModesOrder)
	}
}
