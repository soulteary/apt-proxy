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

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
)

// deepinType is the type the custom distribution of these tests starts with.
const deepinType = 6

// deepinDistribution is a distributions.yaml that defines one distribution
// apt-proxy does not ship, with mirror as its only mirror.
func deepinDistribution(mirror string, distType int) string {
	return fmt.Sprintf(`
distributions:
  - id: deepin
    name: Deepin
    type: %d
    url_pattern: "/deepin/(.+)$"
    benchmark_url: "dists/apricot/Release"
    cache_rules:
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "%s/deepin/"
`, distType, mirror)
}

// pathLog records the paths a mirror was asked for.
type pathLog struct {
	mu    sync.Mutex
	paths []string
}

func (l *pathLog) add(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paths = append(l.paths, path)
}

// distributions returns the first path segment of every recorded request.
func (l *pathLog) distributions() map[string]bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := make(map[string]bool)
	for _, path := range l.paths {
		segment, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		seen[segment] = true
	}
	return seen
}

// modeSetMirror starts the mirror every distribution of these tests uses. It
// answers every path, the election probe of the custom distribution included,
// and forbids caching so that each client request reaches it.
func modeSetMirror(t *testing.T) (*httptest.Server, *pathLog) {
	t.Helper()
	log := &pathLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "mirror "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server, log
}

// mirrorsOn pins a mirror for every built-in distribution, enabled or not, so
// that no test elects a built-in mirror over the network.
func mirrorsOn(mirror string) config.MirrorConfig {
	return config.MirrorConfig{
		Ubuntu: mirror + "/ubuntu/", UbuntuPorts: mirror + "/ubuntu-ports/", Debian: mirror + "/debian/",
		CentOS: mirror + "/centos/", Alpine: mirror + "/alpine/",
	}
}

// awaitElection waits for the background mirror election of a custom
// distribution, which NewServer starts, so that it does not outlive the test.
func awaitElection(t *testing.T, srv *Server, distType int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := srv.proxy.BenchmarkEngine().Cache().GetCachedResult(distType); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("mirror election of distribution type %d did not finish", distType)
		}
		time.Sleep(time.Millisecond)
	}
}

// The daemon serves the distributions named in the mode, built-in and custom,
// and answers 404 for every other built-in distribution in both request
// forms. This is the configuration issue #182 asks for.
func TestDaemonServesTheConfiguredModeSet(t *testing.T) {
	mirror, requested := modeSetMirror(t)
	// The host a proxy client names. It is no mirror and must not be contacted.
	named, namedRequests := modeSetMirror(t)

	distributions := filepath.Join(t.TempDir(), "distributions.yaml")
	writeAt(t, distributions, deepinDistribution(mirror.URL, deepinType))
	srv, err := NewServer(&config.Config{
		CacheDir: t.TempDir(), Listen: "127.0.0.1:0",
		Modes:                   []string{"debian", "deepin"},
		Mirrors:                 mirrorsOn(mirror.URL),
		DistributionsConfigPath: distributions,
	})
	if err != nil {
		t.Fatal(err)
	}
	address := listenOn(t, srv)
	awaitElection(t, srv, deepinType)

	proxied := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: address})}
	defer proxied.CloseIdleConnections()
	direct := &http.Transport{}
	defer direct.CloseIdleConnections()

	for _, tc := range []struct {
		path string
		// wantBody is empty for a path that must answer 404.
		wantBody string
	}{
		{"/debian/dists/x/InRelease", "mirror /debian/dists/x/InRelease"},
		{"/deepin/dists/apricot/InRelease", "mirror /deepin/dists/apricot/InRelease"},
		{"/ubuntu/dists/x/InRelease", ""},
		{"/ubuntu-ports/dists/x/InRelease", ""},
		{"/centos/9-stream/BaseOS/x86_64/os/repodata/repomd.xml", ""},
		{"/alpine/v3.22/main/x86_64/APKINDEX.tar.gz", ""},
	} {
		for form, request := range map[string]struct {
			transport *http.Transport
			target    string
		}{
			// A proxy client names another host in the request line.
			"absolute form": {proxied, named.URL + tc.path},
			// A direct client addresses the daemon.
			"origin form": {direct, "http://" + address + tc.path},
		} {
			t.Run(tc.path+"/"+form, func(t *testing.T) {
				res, err := (&http.Client{Timeout: 10 * time.Second, Transport: request.transport}).Get(request.target)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantBody == "" {
					if res.StatusCode != http.StatusNotFound {
						t.Fatalf("status = %d, body = %q; want 404", res.StatusCode, body)
					}
					return
				}
				if res.StatusCode != http.StatusOK || string(body) != tc.wantBody {
					t.Fatalf("status = %d, body = %q; want 200 %q", res.StatusCode, body, tc.wantBody)
				}
			})
		}
	}

	// The mirror was asked for the two enabled distributions only: neither a
	// client request nor an election reached it for the others.
	if got := requested.distributions(); len(got) != 2 || !got["debian"] || !got["deepin"] {
		t.Errorf("the mirror received requests for %v, want debian and deepin only", got)
	}
	if got := namedRequests.distributions(); len(got) != 0 {
		t.Errorf("the host named by the client received requests for %v", got)
	}
}

// A mode the registry cannot satisfy stops the startup, before the proxy is
// built. The error names the value and lists the IDs that exist, including
// those from distributions.yaml.
func TestServerRejectsUnknownMode(t *testing.T) {
	// The file is never served from, so its mirror is not contacted.
	withDeepin := deepinDistribution("http://mirror.invalid", deepinType)
	const builtins = "alpine, centos, debian, ubuntu, ubuntu-ports"

	for _, tc := range []struct {
		name          string
		modes         []string
		distributions string
		want          []string
	}{
		{
			name:  "typo of a built-in",
			modes: []string{"debain"}, distributions: withDeepin,
			want: []string{`invalid mode: unknown distribution "debain"`, "alpine, centos, debian, deepin, ubuntu, ubuntu-ports"},
		},
		{
			name:  "custom ID with a typo",
			modes: []string{"debian", "deepin-v23"}, distributions: withDeepin,
			want: []string{`unknown distribution "deepin-v23"`, "deepin, ubuntu"},
		},
		{
			name:  "custom ID the file does not define",
			modes: []string{"debian", "deepin"}, distributions: "distributions: []\n",
			want: []string{`unknown distribution "deepin"`, builtins},
		},
		{
			// The file is rejected with a warning and the built-ins stay, as
			// before. The mode that needs the file cannot be served.
			name:  "custom ID with an unparseable file",
			modes: []string{"deepin"}, distributions: "distributions: [ this is not: valid yaml",
			want: []string{`unknown distribution "deepin"`, builtins},
		},
		{
			name:  "all combined with a distribution",
			modes: []string{"all", "debian"}, distributions: withDeepin,
			want: []string{`"all" cannot be combined with other distributions (got all, debian)`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "distributions.yaml")
			writeAt(t, path, tc.distributions)
			s := &Server{config: withTestMirrors(&config.Config{
				CacheDir: t.TempDir(), Listen: "127.0.0.1:0",
				Modes: tc.modes, DistributionsConfigPath: path,
			})}
			s.initLogger()
			err := s.initialize()
			if s.cache != nil {
				defer s.cache.Close()
			}
			if err == nil {
				t.Fatal("the server initialized with a mode it cannot serve")
			}
			for _, text := range tc.want {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error %q does not contain %q", err, text)
				}
			}
			if s.proxy != nil {
				t.Error("the proxy was built before the mode was validated")
			}
		})
	}
}

// refreshAPI posts to the mirror refresh endpoint and returns the status and
// the decoded response.
func refreshAPI(t *testing.T, srv *Server, key string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mirrors/refresh", nil)
	req.Header.Set("X-API-Key", key)
	res, err := srv.testRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

// A reload re-reads distributions.yaml, and the mode is checked against the
// result. When an enabled ID is gone, the refresh fails with an error that
// names it and the routing in service stays: the distribution keeps being
// served until the configuration is consistent again.
func TestReloadWithVanishedModeIDKeepsPreviousRouting(t *testing.T) {
	mirror, _ := modeSetMirror(t)
	distributions := filepath.Join(t.TempDir(), "distributions.yaml")
	writeAt(t, distributions, deepinDistribution(mirror.URL, deepinType))
	srv, err := NewServer(&config.Config{
		CacheDir: t.TempDir(), Listen: "127.0.0.1:0",
		// The ID is written in another letter case than in the file. It is
		// resolved without regard to case and reported as written.
		Modes:                   []string{"ubuntu", "Deepin"},
		Mirrors:                 mirrorsOn(mirror.URL),
		DistributionsConfigPath: distributions,
		Security:                config.SecurityConfig{EnableAPIAuth: true, APIKey: "refresh-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	awaitElection(t, srv, deepinType)

	// expectRouting checks the two enabled distributions and one that the mode
	// leaves out.
	expectRouting := func(when string) {
		t.Helper()
		for path, wantServed := range map[string]bool{
			"/ubuntu/pool/package.deb": true,
			"/deepin/pool/package.deb": true,
			"/debian/pool/package.deb": false,
		} {
			res, err := srv.testRequest(httptest.NewRequest(http.MethodGet, path, nil))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if wantServed && (res.StatusCode != http.StatusOK || string(body) != "mirror "+path) {
				t.Errorf("%s: GET %s: status = %d, body = %q; want it served from the mirror", when, path, res.StatusCode, body)
			}
			if !wantServed && res.StatusCode != http.StatusNotFound {
				t.Errorf("%s: GET %s: status = %d, want 404", when, path, res.StatusCode)
			}
		}
	}
	expectRouting("at startup")

	// The file no longer defines the enabled custom distribution.
	writeAt(t, distributions, "distributions: []\n")

	status, body := refreshAPI(t, srv, "refresh-key")
	if status != http.StatusBadRequest || body["code"] != "CONFIG_INVALID" {
		t.Fatalf("refresh: status = %d, body = %v; want 400 CONFIG_INVALID", status, body)
	}
	details, _ := body["details"].(map[string]any)
	if reason, _ := details["reason"].(string); !strings.Contains(reason, `unknown distribution "Deepin"`) {
		t.Errorf("refresh response %v does not name the missing distribution", body)
	}
	// The reload that SIGHUP runs reports the same error.
	if err := srv.refreshMirrors(); err == nil || !strings.Contains(err.Error(), `unknown distribution "Deepin"`) {
		t.Errorf("refreshMirrors error = %v, want the missing distribution named", err)
	}
	srv.reload()

	// The registry follows the file, the routing does not.
	if _, ok := srv.registry.GetByID("deepin"); ok {
		t.Fatal("the reloaded registry still defines deepin")
	}
	expectRouting("after the failed reload")

	// With the distribution defined again, the reload goes through.
	writeAt(t, distributions, deepinDistribution(mirror.URL, deepinType))
	if status, body := refreshAPI(t, srv, "refresh-key"); status != http.StatusOK || body["success"] != true {
		t.Fatalf("refresh after restoring the file: status = %d, body = %v", status, body)
	}
	expectRouting("after the repaired reload")
}

// The mode names IDs, and a reload may give an ID another type. The routing
// follows the ID: the set of types resolved at startup is not reused.
func TestReloadFollowsEnabledIDToItsNewType(t *testing.T) {
	mirror, _ := modeSetMirror(t)
	distributions := filepath.Join(t.TempDir(), "distributions.yaml")
	writeAt(t, distributions, deepinDistribution(mirror.URL, deepinType))
	srv, err := NewServer(&config.Config{
		CacheDir: t.TempDir(), Listen: "127.0.0.1:0",
		Modes:                   []string{"deepin"},
		Mirrors:                 mirrorsOn(mirror.URL),
		DistributionsConfigPath: distributions,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	awaitElection(t, srv, deepinType)

	const movedType = deepinType + 3
	writeAt(t, distributions, deepinDistribution(mirror.URL, movedType))
	if err := srv.refreshMirrors(); err != nil {
		t.Fatalf("refreshMirrors: %v", err)
	}

	mode := srv.state.GetProxyMode()
	if !mode.Has(movedType) || mode.Has(deepinType) {
		t.Errorf("enabled types = %v, want the new type of deepin only", mode.Types(srv.registry))
	}
	res, err := srv.testRequest(httptest.NewRequest(http.MethodGet, "/deepin/pool/package.deb", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != "mirror /deepin/pool/package.deb" {
		t.Fatalf("GET /deepin/pool/package.deb: status = %d, body = %q; want it served from the mirror", res.StatusCode, body)
	}
}
