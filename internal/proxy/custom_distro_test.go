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
	"net/url"
	"strings"
	"testing"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// customDistroType is outside the built-in range (1..5), matching what a
// distributions.yaml entry would use.
const customDistroType = 42

func registryWithCustomDistro(t *testing.T, mirror string) *distro.Registry {
	t.Helper()
	reg := distro.NewBuiltinRegistry()
	cfg := &distro.DistributionConfig{
		ID:           "deepin",
		Name:         "Deepin",
		Type:         customDistroType,
		URLPattern:   `/deepin/(.+)$`,
		BenchmarkURL: "dists/apricot/main/binary-amd64/Release",
		CacheRules: []distro.CacheRuleConfig{
			{Pattern: `deb$`, CacheControl: "max-age=100000", Rewrite: true},
			{Pattern: `InRelease$`, CacheControl: "max-age=3600", Rewrite: true},
		},
		Mirrors: distro.MirrorListConfig{Official: []string{mirror}},
	}
	if err := reg.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}
	return reg
}

// localMirror starts a stand-in upstream that answers the benchmark probe, so
// mirror election resolves without reaching the network.
func localMirror(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/deepin/"
}

// A distribution registered from distributions.yaml must get a rewriter, so
// its requests are actually redirected to the configured mirror. Before this
// was wired up the request fell through unrewritten and reached the upstream
// proxy with a relative URL.
func TestCustomDistroIsRewrittenToItsMirror(t *testing.T) {
	mirror := localMirror(t)
	reg := registryWithCustomDistro(t, mirror)
	rewriters := CreateNewRewriters(distro.TypeAllDistros, newTestState(), reg)

	if got := rewriters.get(customDistroType); got == nil {
		t.Fatal("no rewriter built for the registry-defined distribution")
	}

	r := &http.Request{
		Host: "community-packages.deepin.com",
		URL:  &url.URL{Path: "/deepin/dists/apricot/InRelease"},
	}
	RewriteRequestByMode(r, rewriters, customDistroType)

	want := strings.TrimSuffix(mirror, "/deepin/") + "/deepin/dists/apricot/InRelease"
	if got := r.URL.String(); got != want {
		t.Errorf("rewritten URL = %q, want %q", got, want)
	}
}

// The built-in distros must keep working unchanged alongside a custom one.
func TestBuiltinDistrosUnaffectedByCustomRegistration(t *testing.T) {
	reg := registryWithCustomDistro(t, localMirror(t))
	st := newTestState()
	rewriters := CreateNewRewriters(distro.TypeAllDistros, st, reg)

	for _, mode := range []int{
		distro.TypeUbuntu, distro.TypeUbuntuPorts,
		distro.TypeDebian, distro.TypeCentOS, distro.TypeAlpine,
	} {
		if rewriters.get(mode) == nil {
			t.Errorf("built-in mode %d lost its rewriter", mode)
		}
	}
	if rewriters.Ubuntu == nil || rewriters.Debian == nil {
		t.Error("named built-in fields must still be populated")
	}
}

// modesToInit must surface registry types for TypeAllDistros, and stay
// deterministic so reloads don't reorder construction.
func TestModesToInitIncludesRegistryTypes(t *testing.T) {
	reg := registryWithCustomDistro(t, "example.com/deepin/")

	modes := modesToInit(distro.TypeAllDistros, reg)
	found := false
	for _, m := range modes {
		if m == customDistroType {
			found = true
		}
	}
	if !found {
		t.Fatalf("modesToInit(all) = %v, missing custom type %d", modes, customDistroType)
	}

	for i := 0; i < 5; i++ {
		if got := modesToInit(distro.TypeAllDistros, reg); len(got) != len(modes) {
			t.Fatalf("modesToInit not deterministic: %v vs %v", got, modes)
		}
	}

	// A specific mode still resolves to just that mode.
	if got := modesToInit(distro.TypeDebian, reg); len(got) != 1 || got[0] != distro.TypeDebian {
		t.Errorf("modesToInit(debian) = %v, want [%d]", got, distro.TypeDebian)
	}
	// A nil registry must not panic and must yield the built-ins.
	if got := modesToInit(distro.TypeAllDistros, nil); len(got) != len(distroModesOrder) {
		t.Errorf("modesToInit(all, nil) = %v, want the %d built-ins", got, len(distroModesOrder))
	}
}

// resolveDescriptor synthesises a descriptor for registry-only types and
// still returns nil for types nobody knows about.
func TestResolveDescriptorForRegistryType(t *testing.T) {
	reg := registryWithCustomDistro(t, "example.com/deepin/")

	d, name := resolveDescriptor(customDistroType, reg)
	if d == nil {
		t.Fatal("expected a synthesised descriptor for the registry type")
	}
	if name != "Deepin" {
		t.Errorf("name = %q, want %q", name, "Deepin")
	}
	if len(d.defaultRules) != 2 {
		t.Errorf("defaultRules = %d, want 2 (from the YAML cache_rules)", len(d.defaultRules))
	}
	if d.getMirror == nil {
		t.Error("getMirror must be set so mirror selection can run")
	}

	if d, _ := resolveDescriptor(9999, reg); d != nil {
		t.Error("unknown type must not resolve to a descriptor")
	}
}

// RefreshMirrors (SIGHUP / POST /api/mirrors/refresh) must rebuild custom
// distro rewriters too, not just the built-ins.
func TestRefreshRewritersKeepsCustomDistro(t *testing.T) {
	reg := registryWithCustomDistro(t, localMirror(t))
	st := newTestState()
	rewriters := CreateNewRewriters(distro.TypeAllDistros, st, reg)

	RefreshRewriters(rewriters, distro.TypeAllDistros, st, reg)

	if rewriters.get(customDistroType) == nil {
		t.Fatal("custom distro rewriter dropped by refresh")
	}
}

// End-to-end through the real handler: a request for a distributions.yaml
// distribution must reach that distro's mirror. This exercises the whole
// chain (pattern match -> cache rule -> rewrite -> upstream) using only the
// exported API, so it stays meaningful independent of the internal rewriter
// plumbing.
func TestCustomDistroReachesUpstreamEndToEnd(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	reg := registryWithCustomDistro(t, upstream.URL+"/deepin/")
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

	req := httptest.NewRequest(http.MethodGet, "http://community-packages.deepin.com/deepin/dists/apricot/InRelease", nil)
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (request never reached the custom mirror)", rec.Code)
	}
	if want := "/deepin/dists/apricot/InRelease"; gotPath != want {
		t.Errorf("upstream saw path %q, want %q", gotPath, want)
	}
}
