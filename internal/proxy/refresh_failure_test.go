package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// electionMirror starts a mirror that answers every request with name and the
// requested path, or with 503 while down is set.
func electionMirror(t *testing.T, name string, down *atomic.Bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down != nil && down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, name+" "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server
}

// soleCandidate makes mirror the only candidate mode can elect.
func soleCandidate(t *testing.T, reg *distro.Registry, mode int, mirror string) *distro.RegisteredDistribution {
	t.Helper()
	d, ok := reg.GetByType(mode)
	if !ok {
		t.Fatalf("distribution type %d is not registered", mode)
	}
	d.Mirrors = []distro.URLWithAlias{{URL: mirror, Scheme: "http"}}
	return d
}

// serve sends a GET for path through ps and returns the status and the body.
func serve(ps *PackageStruct, path string) (int, string) {
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

// A distribution whose election fails during a refresh keeps its mirror and
// still gets its reloaded patterns, and the refresh goes through for the
// distributions whose election succeeds.
func TestFailedMirrorElectionKeepsPreviousMirror(t *testing.T) {
	var debianDown atomic.Bool
	debian := electionMirror(t, "debian", &debianDown)
	alpineOld := electionMirror(t, "alpine-old", nil)
	alpineNew := electionMirror(t, "alpine-new", nil)

	reg := newTestRegistry()
	debianDist := soleCandidate(t, reg, distro.TypeDebian, debian.URL+"/debian/")
	soleCandidate(t, reg, distro.TypeAlpine, alpineOld.URL+"/alpine/")
	st := newTestState()
	st.SetMirror(distro.TypeDebian, "") // Elect rather than pin the mirror.
	st.SetMirror(distro.TypeAlpine, "")
	ps, err := NewPackageStruct(Options{State: st, Registry: reg, Mode: distro.TypeAllDistros})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := serve(ps, "/debian/pool/package.deb"); code != 200 || body != "debian /debian/pool/package.deb" {
		t.Fatalf("initial Debian route: status=%d body=%q", code, body)
	}
	if code, body := serve(ps, "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz"); code != 200 || body != "alpine-old /alpine/v3.22/main/x86_64/APKINDEX.tar.gz" {
		t.Fatalf("initial Alpine route: status=%d body=%q", code, body)
	}

	// The reload renames the Debian paths while the Debian mirror is down, and
	// moves Alpine to another mirror.
	reloaded := *debianDist
	reloaded.URLPattern = regexp.MustCompile(`/(reloaded-debian|debian-security)/(.+)$`)
	if err := reg.Register(&reloaded); err != nil {
		t.Fatal(err)
	}
	soleCandidate(t, reg, distro.TypeAlpine, alpineNew.URL+"/alpine/")
	debianDown.Store(true)

	var logs bytes.Buffer
	defaultLogger := logger.Default()
	logger.SetDefault(logger.New(logger.Config{Level: logger.InfoLevel, Output: &logs, Format: logger.FormatJSON}))
	t.Cleanup(func() { logger.SetDefault(defaultLogger) })
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("one failed election failed the whole refresh: %v", err)
	}
	logger.SetDefault(defaultLogger)
	debianDown.Store(false)

	if code, body := serve(ps, "/reloaded-debian/pool/package.deb"); code != 200 || body != "debian /debian/pool/package.deb" {
		t.Errorf("reloaded Debian route on the previous mirror: status=%d body=%q", code, body)
	}
	if code, body := serve(ps, "/debian-security/pool/package.deb"); code != 200 || body != "debian /debian-security/pool/package.deb" {
		t.Errorf("Debian security route on the previous mirror: status=%d body=%q", code, body)
	}
	if code, _ := serve(ps, "/debian/pool/package.deb"); code != http.StatusNotFound {
		t.Errorf("retired Debian pattern still routes: status=%d", code)
	}
	if code, body := serve(ps, "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz"); code != 200 || body != "alpine-new /alpine/v3.22/main/x86_64/APKINDEX.tar.gz" {
		t.Errorf("Alpine route after its own election: status=%d body=%q", code, body)
	}

	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if entry["message"] == "mirror election failed, keeping previous mirror" {
			if entry["level"] != "warn" || entry["mirror"] != debian.URL+"/debian/" {
				t.Errorf("unexpected warning: %s", line)
			}
			kept = append(kept, entry["distro"].(string))
		}
	}
	if len(kept) != 1 || kept[0] != "Debian" {
		t.Errorf("distributions warned about keeping their mirror = %v, want [Debian]", kept)
	}
}

// A distribution without a previous mirror has nothing to keep: its failed
// election still fails the refresh, and the complete previous routing stays.
func TestFailedMirrorElectionWithoutPreviousMirrorRetainsRouting(t *testing.T) {
	var down atomic.Bool
	upstream := electionMirror(t, "upstream", &down)

	reg := newTestRegistry()
	st := newTestState()
	st.SetMirror(distro.TypeDebian, upstream.URL+"/debian/")
	ps, err := NewPackageStruct(Options{State: st, Registry: reg, Mode: distro.TypeAllDistros})
	if err != nil {
		t.Fatal(err)
	}
	before := ps.routing.Load()

	// The reload renames the Debian paths and adds a distribution whose only
	// mirror is down.
	debianDist, _ := reg.GetByType(distro.TypeDebian)
	reloaded := *debianDist
	reloaded.URLPattern = regexp.MustCompile(`/reloaded-debian/(.+)$`)
	if err := reg.Register(&reloaded); err != nil {
		t.Fatal(err)
	}
	if err := reg.LoadFromConfig(&distro.DistributionConfig{
		ID: "deepin", Name: "Deepin", Type: customDistroType,
		URLPattern: `/deepin/(.+)$`, BenchmarkURL: "probe",
		CacheRules: []distro.CacheRuleConfig{{Pattern: `deb$`, Rewrite: true}},
		Mirrors:    distro.MirrorListConfig{Official: []string{upstream.URL + "/deepin/"}},
	}); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	if err := ps.RefreshMirrors(); err == nil {
		t.Fatal("failed election reported success")
	}
	if ps.routing.Load() != before {
		t.Fatal("failed election replaced the working routing snapshot")
	}
	if err := RefreshRewritersWithEngine(before.rewriters, distro.ModeOf(distro.TypeAllDistros), st, reg, ps.bench); err == nil {
		t.Fatal("in-place failed election reported success")
	}
	if before.rewriters.published(customDistroType) != nil {
		t.Fatal("in-place failed election published part of the new set")
	}
	if _, err := buildRewriters(nil, distro.ModeOf(distro.TypeAllDistros), st, reg, ps.bench); err == nil {
		t.Fatal("failed election without a previous set reported success")
	}
	down.Store(false)

	if code, body := serve(ps, "/debian/pool/package.deb"); code != 200 || body != "upstream /debian/pool/package.deb" {
		t.Fatalf("restored upstream lost old route: status=%d body=%q", code, body)
	}
	for _, path := range []string{"/reloaded-debian/pool/package.deb", "/deepin/pool/package.deb"} {
		if code, _ := serve(ps, path); code != http.StatusNotFound {
			t.Fatalf("failed reload published new patterns: %s status=%d", path, code)
		}
	}
	if err := ps.RefreshMirrors(); err != nil {
		t.Fatalf("healthy election: %v", err)
	}
	if code, body := serve(ps, "/reloaded-debian/pool/package.deb"); code != 200 || body != "upstream /debian/pool/package.deb" {
		t.Fatalf("successful retry did not publish the reloaded route: status=%d body=%q", code, body)
	}
	if code, body := serve(ps, "/deepin/pool/package.deb"); code != 200 || body != "upstream /deepin/pool/package.deb" {
		t.Fatalf("successful retry did not publish the new distribution: status=%d body=%q", code, body)
	}
}

// RefreshRewritersWithEngine applies the same rule to the set it is given.
func TestRefreshRewritersKeepsPreviousMirrorOnFailedElection(t *testing.T) {
	var down atomic.Bool
	upstream := electionMirror(t, "upstream", &down)

	reg := newTestRegistry()
	debianDist := soleCandidate(t, reg, distro.TypeDebian, upstream.URL+"/debian/")
	st := newTestState()
	st.SetMirror(distro.TypeDebian, "") // Elect rather than pin the mirror.
	engine := benchmarks.NewEngine()
	rewriters := CreateNewRewritersWithEngine(distro.ModeOf(distro.TypeDebian), st, reg, engine)
	before := rewriters.published(distro.TypeDebian)
	if before == nil || before.mirror == nil || before.mirror.String() != upstream.URL+"/debian/" {
		t.Fatalf("initial election: %+v", before)
	}

	reloaded := *debianDist
	reloaded.URLPattern = regexp.MustCompile(`/reloaded-debian/(.+)$`)
	if err := reg.Register(&reloaded); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	if err := RefreshRewritersWithEngine(rewriters, distro.ModeOf(distro.TypeDebian), st, reg, engine); err != nil {
		t.Fatalf("failed election with a previous mirror: %v", err)
	}
	after := rewriters.published(distro.TypeDebian)
	if after.mirror != before.mirror || after.securityMirror != before.securityMirror {
		t.Errorf("mirrors after the failed election = %v, %v; want the previous %v, %v", after.mirror, after.securityMirror, before.mirror, before.securityMirror)
	}
	if after.pattern != reloaded.URLPattern {
		t.Errorf("pattern after the failed election = %v, want the reloaded %v", after.pattern, reloaded.URLPattern)
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
