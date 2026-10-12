package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestFailedMirrorElectionRetainsWorkingRouting(t *testing.T) {
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "package")
	}))
	defer upstream.Close()
	reg := newTestRegistry()
	d, _ := reg.GetByType(distro.TypeDebian)
	d.Mirrors = []distro.URLWithAlias{{URL: upstream.URL + "/debian/", Scheme: "http"}}
	st := newTestState()
	st.SetMirror(distro.TypeDebian, "") // Elect rather than pin the mirror.
	ps, err := NewPackageStruct(Options{State: st, Registry: reg, Mode: distro.TypeDebian})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if res := get("/debian/pool/package.deb"); res.Code != 200 || res.Body.String() != "package" {
		t.Fatalf("initial route: status=%d body=%q", res.Code, res.Body.String())
	}
	before := ps.routing.Load()
	// A reloaded pattern must not go live when its new election fails either.
	reloaded := *d
	reloaded.URLPattern = regexp.MustCompile(`/changed-debian/(.+)$`)
	if err := reg.Register(&reloaded); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if err := ps.RefreshMirrors(); err == nil {
		t.Fatal("failed election reported success")
	}
	if ps.routing.Load() != before {
		t.Fatal("failed election replaced the working routing snapshot")
	}
	if err := RefreshRewritersWithEngine(before.rewriters, distro.TypeDebian, st, reg, ps.bench); err == nil {
		t.Fatal("in-place failed election reported success")
	}
	fail.Store(false)
	if res := get("/debian/pool/package.deb"); res.Code != 200 || res.Body.String() != "package" {
		t.Fatalf("restored upstream lost old route: status=%d body=%q", res.Code, res.Body.String())
	}
	if res := get("/changed-debian/pool/package.deb"); res.Code != http.StatusNotFound {
		t.Fatalf("failed reload published new patterns: status=%d", res.Code)
	}
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("healthy election: %v", err)
	}
	if res := get("/changed-debian/pool/package.deb"); res.Code != 200 {
		t.Fatalf("successful retry did not publish new route: status=%d", res.Code)
	}
}

func TestOverlappingCustomDistributionsHaveStablePriority(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer upstream.Close()
	reg := newTestRegistry()
	// Register in reverse ID order; IDs, not map order or type number, decide
	// which overlapping custom distribution wins.
	for _, entry := range []struct {
		id   string
		mode int
	}{{"z-last", 6}, {"a-first", 42}} {
		if err := reg.LoadFromConfig(&distro.DistributionConfig{
			ID: entry.id, Name: entry.id, Type: entry.mode,
			URLPattern: `/custom/(.+)$`, BenchmarkURL: "probe",
			CacheRules: []distro.CacheRuleConfig{{Pattern: `deb$`, Rewrite: true}},
			Mirrors:    distro.MirrorListConfig{Official: []string{upstream.URL + "/" + entry.id + "/"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := NewPackageStruct(Options{State: newTestState(), Registry: reg, Mode: distro.TypeAllDistros})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		rec := httptest.NewRecorder()
		ps.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom/package.deb", nil))
		if rec.Code != 200 || rec.Body.String() != "/a-first/package.deb" {
			t.Fatalf("iteration %d: unstable custom priority: status=%d body=%q", i, rec.Code, rec.Body.String())
		}
		if err := ps.RefreshMirrors(); err != nil {
			t.Fatal(err)
		}
	}
}
