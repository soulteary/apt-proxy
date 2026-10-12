package mirrorhealth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
)

type fixture struct {
	server              *httptest.Server
	fail                atomic.Bool
	requests            atomic.Int64
	packages            atomic.Int64
	mu                  sync.Mutex
	manifests           map[string]string
	lastPath, lastQuery string
	manifestStatus      int
	manifestHeader      http.Header
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{manifests: map[string]string{"jammy": "same signed snapshot", "focal": "same older snapshot"}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if f.fail.Load() {
			w.WriteHeader(503)
			_, _ = io.WriteString(w, "primary unavailable")
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.lastPath, f.lastQuery = r.URL.EscapedPath(), r.URL.RawQuery
		if strings.Contains(r.URL.Path, "/dists/") && (strings.HasSuffix(r.URL.Path, "/InRelease") || strings.HasSuffix(r.URL.Path, "/Release")) {
			for key, values := range f.manifestHeader {
				w.Header()[key] = append([]string(nil), values...)
			}
			if f.manifestStatus != 0 {
				w.WriteHeader(f.manifestStatus)
				return
			}
			parts := strings.Split(r.URL.Path, "/")
			_, _ = io.WriteString(w, f.manifests[parts[len(parts)-2]])
			return
		}
		f.packages.Add(1)
		_, _ = io.WriteString(w, "complete package")
	}))
	t.Cleanup(f.server.Close)
	return f
}

func newController(t *testing.T, primary, secondary *fixture) *Controller {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.server.URL + "/alternate/"}, PublicQueryKeys: []string{"download"}}}}, transport, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fetch(t *testing.T, c *Controller, primary *fixture, path string, mutate func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, primary.server.URL+"/ubuntu/"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(req)
	}
	res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

func learn(t *testing.T, c *Controller, primary *fixture, suite string) {
	t.Helper()
	if status, _ := fetch(t, c, primary, "dists/"+suite+"/InRelease", nil); status != 200 {
		t.Fatal(status)
	}
}

func TestRuntimeFailoverRequiresKnownIdenticalSnapshot(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	primary.fail.Store(true)
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 503 || secondary.requests.Load() != 0 {
		t.Fatal("unknown snapshot permitted a switch")
	}
	primary.fail.Store(false)
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	query := "download=%2F&download=a+b"
	if status, body := fetch(t, c, primary, "pool/a%20b.deb?"+query, nil); status != 200 || body != "complete package" {
		t.Fatalf("failover failed: %d %q", status, body)
	}
	secondary.mu.Lock()
	path, gotQuery := secondary.lastPath, secondary.lastQuery
	secondary.mu.Unlock()
	if path != "/alternate/pool/a%20b.deb" || gotQuery != query {
		t.Fatalf("mapping changed URL: %q ? %q", path, gotQuery)
	}
	before := primary.requests.Load()
	if status, _ := fetch(t, c, primary, "pool/another.deb", func(r *http.Request) { r.Method = http.MethodHead }); status != 200 || primary.requests.Load() != before {
		t.Fatal("verified replacement was not used for HEAD")
	}
	if status, _ := fetch(t, c, primary, "dists/unknown/InRelease", nil); status != 503 {
		t.Fatal("a new suite borrowed another suite's snapshot")
	}
	state := c.State().Repositories[0]
	if state.Active != secondary.server.URL+"/alternate/" || state.SnapshotCount != 1 {
		t.Fatalf("missing observable selection: %+v", state)
	}
}

func TestRuntimeFailoverRejectsCandidateMetadata(t *testing.T) {
	for _, kind := range []string{"divergent", "missing", "redirect", "private", "cookie", "large"} {
		t.Run(kind, func(t *testing.T) {
			primary, secondary := newFixture(t), newFixture(t)
			c := newController(t, primary, secondary)
			learn(t, c, primary, "jammy")
			secondary.mu.Lock()
			switch kind {
			case "divergent":
				secondary.manifests["jammy"] = "lagging snapshot"
			case "missing":
				secondary.manifestStatus = 404
			case "redirect":
				secondary.manifestStatus = http.StatusFound
			case "private":
				secondary.manifestHeader = http.Header{"Cache-Control": []string{"private"}}
			case "cookie":
				secondary.manifestHeader = http.Header{"Set-Cookie": []string{"session=secret"}}
			case "large":
				secondary.manifests["jammy"] = strings.Repeat("x", int(c.cfg.MaxManifestBytes)+1)
			}
			secondary.mu.Unlock()
			primary.fail.Store(true)
			if status, body := fetch(t, c, primary, "pool/package.deb", nil); status != 503 || body != "primary unavailable" || secondary.packages.Load() != 0 {
				t.Fatalf("rejected mirror served data: status=%d body=%q", status, body)
			}
			before := secondary.requests.Load()
			fetch(t, c, primary, "pool/package.deb", nil)
			if secondary.requests.Load() != before || c.State().Repositories[0].LastDecision != "cooldown" {
				t.Fatal("failed selection ignored cooldown")
			}
		})
	}
}

func TestRuntimeFailoverVerifiesAllObservedSuitesForPool(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	learn(t, c, primary, "jammy")
	learn(t, c, primary, "focal")
	secondary.mu.Lock()
	secondary.manifests["focal"] = "different focal"
	secondary.mu.Unlock()
	primary.fail.Store(true)
	if status, _ := fetch(t, c, primary, "pool/shared.deb", nil); status != 503 || secondary.packages.Load() != 0 {
		t.Fatal("pool failover ignored another suite's snapshot")
	}
}

func TestRuntimeFailoverNeverForwardsPrivateRequests(t *testing.T) {
	for _, kind := range []string{"authorization", "cookie", "userinfo", "custom-token", "unknown-query"} {
		t.Run(kind, func(t *testing.T) {
			primary, secondary := newFixture(t), newFixture(t)
			c := newController(t, primary, secondary)
			learn(t, c, primary, "jammy")
			primary.fail.Store(true)
			mutate := func(r *http.Request) {
				switch kind {
				case "authorization":
					r.Header.Set("Authorization", "Bearer secret")
				case "cookie":
					r.Header.Set("Cookie", "session=secret")
				case "userinfo":
					r.URL.User = url.UserPassword("token", "secret")
				case "custom-token":
					r.Header.Set("X-Repository-Token", "secret")
				case "unknown-query":
					r.URL.RawQuery = "token=secret"
				}
			}
			if status, _ := fetch(t, c, primary, "pool/private.deb", mutate); status != 503 || secondary.requests.Load() != 0 {
				t.Fatal("private request reached an alternate host")
			}
		})
	}
}

func TestRuntimeFailoverDiscardsOldEpochObservations(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	req, _ := http.NewRequest(http.MethodGet, primary.server.URL+"/ubuntu/dists/jammy/InRelease", nil)
	old := c.Epoch()
	res, err := c.RoundTrip(req, "ubuntu", old)
	if err != nil {
		t.Fatal(err)
	}
	c.Reset()
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if c.State().Repositories[0].SnapshotCount != 0 {
		t.Fatal("old body contaminated new routing generation")
	}
	res, err = c.RoundTrip(req, "ubuntu", old)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if c.State().Repositories[0].SnapshotCount != 0 {
		t.Fatal("old request contaminated new routing generation")
	}
	learn(t, c, primary, "jammy")
	if c.State().Repositories[0].SnapshotCount != 1 {
		t.Fatal("new epoch did not learn")
	}
}

func TestRuntimeFailoverProbesUseUpstreamPolicy(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	primaryURL, _ := url.Parse(primary.server.URL)
	port := primaryURL.Port()
	// Permit only the primary's port; even loopback exceptions cannot let a
	// secondary probe evade the authority policy.
	var number int
	for _, ch := range port {
		number = number*10 + int(ch-'0')
	}
	policy, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedPorts: []int{number}, AllowCIDRs: []string{"127.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	base := &http.Transport{}
	t.Cleanup(base.CloseIdleConnections)
	guarded := policy.Transport(base)
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.server.URL + "/alternate/"}}}}, guarded, guarded, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 503 || secondary.requests.Load() != 0 {
		t.Fatal("probe escaped upstream access policy")
	}
}

func TestRuntimeFailoverCancellationStopsProbe(t *testing.T) {
	primary := newFixture(t)
	entered := make(chan struct{})
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer secondary.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.URL + "/alternate/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", primary.server.URL+"/ubuntu/pool/package.deb", nil)
	done := make(chan error, 1)
	go func() { res, err := c.RoundTrip(req, "ubuntu", c.Epoch()); closeResponse(res); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe never arrived")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation ignored")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe ignored cancellation")
	}
}

func TestRuntimeFailoverTransportErrorAndThreshold(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	c.cfg.FailureThreshold = 2
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 503 || secondary.requests.Load() != 0 {
		t.Fatal("threshold was ignored")
	}
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 200 {
		t.Fatal("threshold did not permit verified failover")
	}
	// A distinct controller learns before the primary disappears completely.
	primary.fail.Store(false)
	c = newController(t, primary, secondary)
	learn(t, c, primary, "jammy")
	primary.server.Close()
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 200 {
		t.Fatal("transport error did not fail over")
	}
}

func TestRuntimeFailoverDoesNotLearnPartialOrOversizedBodies(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	req, _ := http.NewRequest("GET", primary.server.URL+"/ubuntu/dists/jammy/InRelease", nil)
	res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = res.Body.Read(make([]byte, 1))
	res.Body.Close()
	if c.State().Repositories[0].SnapshotCount != 0 {
		t.Fatal("partial metadata authorized failover")
	}
	learn(t, c, primary, "jammy")
	c.cfg.MaxManifestBytes = 2
	learn(t, c, primary, "jammy")
	if c.State().Repositories[0].SnapshotCount != 0 {
		t.Fatal("over-limit metadata authorized failover")
	}
}

func TestRuntimeFailoverRetainsCallerMetadataLocally(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	var leaked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-Id") != "" {
			leaked.Store(true)
		}
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			_, _ = io.WriteString(w, "same signed snapshot")
			return
		}
		_, _ = io.WriteString(w, "package")
	}))
	defer server.Close()
	c := newController(t, primary, secondary)
	c.repos["ubuntu"].bases[1], _ = url.Parse(server.URL + "/alternate/")
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	if status, _ := fetch(t, c, primary, "pool/package.deb", func(r *http.Request) { r.Header["x-request-id"] = []string{"caller-id"} }); status != 200 || leaked.Load() {
		t.Fatal("operational header crossed mirror roots")
	}
}

func TestRuntimeFailoverTotalProbeDeadline(t *testing.T) {
	primary := newFixture(t)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, ProbeTimeoutSeconds: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", slow.URL + "/archive/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	start := time.Now()
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 503 {
		t.Fatal("timed-out candidate accepted")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("probe selection exceeded its bounded total deadline")
	}
}

func TestRuntimeFailoverMetadataReplayRetainsVerifiedReplacement(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	if status, body := fetch(t, c, primary, "dists/jammy/InRelease", nil); status != 200 || body != "same signed snapshot" {
		t.Fatal("metadata itself could not fail over")
	}
	before := primary.requests.Load()
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 200 || primary.requests.Load() != before {
		t.Fatal("completed metadata replay lost replacement verification")
	}
}

func TestRuntimeFailoverExpiredSnapshotAndNon5xx(t *testing.T) {
	primary, secondary := newFixture(t), newFixture(t)
	c := newController(t, primary, secondary)
	learn(t, c, primary, "jammy")
	repo := c.repos["ubuntu"]
	repo.mu.Lock()
	snap := repo.snapshots["dists/jammy/InRelease"]
	snap.observed = time.Now().Add(-2 * time.Hour)
	repo.snapshots["dists/jammy/InRelease"] = snap
	repo.mu.Unlock()
	primary.fail.Store(true)
	if status, _ := fetch(t, c, primary, "pool/package.deb", nil); status != 503 || secondary.requests.Load() != 0 {
		t.Fatal("expired fingerprint permitted switch")
	}
	// A 404 is never a health-triggered failover, even with a fresh fingerprint.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			_, _ = io.WriteString(w, "same signed snapshot")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{server.URL + "/ubuntu/", secondary.server.URL + "/archive/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	primary.server = server
	learn(t, c, primary, "jammy")
	if status, _ := fetch(t, c, primary, "pool/missing.deb", nil); status != 404 || secondary.requests.Load() != 0 {
		t.Fatal("404 triggered alternate fetch")
	}
}

func TestRuntimeFailoverConcurrentRequestsShareSelection(t *testing.T) {
	primary := newFixture(t)
	var probes atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			probes.Add(1)
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = io.WriteString(w, "same signed snapshot")
			return
		}
		_, _ = io.WriteString(w, "package")
	}))
	defer secondary.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.URL + "/archive/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		go func() {
			req, _ := http.NewRequestWithContext(ctx, "GET", primary.server.URL+"/ubuntu/pool/package.deb", nil)
			res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
			if err != nil {
				closeResponse(res)
				results <- false
				return
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			results <- err == nil && res.StatusCode == 200 && string(body) == "package"
		}()
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("probe never arrived")
	}
	for primary.requests.Load() < 17 {
		select {
		case <-ctx.Done():
			t.Fatal("requests never reached the failed source")
		case <-time.After(time.Millisecond):
		}
	}
	unblock()
	for i := 0; i < 16; i++ {
		select {
		case ok := <-results:
			if !ok {
				t.Fatal("concurrent request failed")
			}
		case <-ctx.Done():
			t.Fatal("selection stranded a follower")
		}
	}
	if probes.Load() != 1 {
		t.Fatalf("concurrent requests issued %d duplicate probes", probes.Load())
	}
}

func TestRuntimeFailoverRefreshRejectsInFlightSelection(t *testing.T) {
	primary := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer func() { once.Do(func() { close(release) }) }()
	var packages atomic.Int64
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/InRelease") {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = io.WriteString(w, "same signed snapshot")
			return
		}
		packages.Add(1)
		_, _ = io.WriteString(w, "package")
	}))
	defer secondary.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	c, err := New(&Config{Enabled: true, FailureThreshold: 1, Repositories: []Repository{{ID: "ubuntu", Mirrors: []string{primary.server.URL + "/ubuntu/", secondary.URL + "/archive/"}}}}, base, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	learn(t, c, primary, "jammy")
	primary.fail.Store(true)
	done := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("GET", primary.server.URL+"/ubuntu/pool/package.deb", nil)
		res, err := c.RoundTrip(req, "ubuntu", c.Epoch())
		if err != nil {
			closeResponse(res)
			done <- 0
			return
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		done <- res.StatusCode
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe never arrived")
	}
	c.Reset()
	once.Do(func() { close(release) })
	select {
	case status := <-done:
		if status != 503 {
			t.Fatalf("stale selection returned %d", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("selection did not finish")
	}
	state := c.State().Repositories[0]
	if state.Active != "" || state.SnapshotCount != 0 || packages.Load() != 0 {
		t.Fatalf("stale selection changed new epoch: %+v", state)
	}
}
