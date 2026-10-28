// Copyright 2026 Su Yang
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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func newRepositoryRoutingOrigin(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Origin", name)
		_, _ = fmt.Fprintf(w, "%s %s\n", r.Host, r.URL.RequestURI())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Issue #167 uses a custom repository whose /debian/ path also matches the
// built-in Debian distribution. Both stand-in origins return 200: checking
// status alone would miss the original bug, which reached the wrong archive.
func TestProxmoxOverlappingDistributionRoutesToConfiguredOrigin(t *testing.T) {
	debian := newRepositoryRoutingOrigin(t, "debian")
	proxmox := newRepositoryRoutingOrigin(t, "proxmox")

	// Load the reporter's overlapping path and explicit host through YAML.
	// A distinct local alias also remains available to URL-prefix clients.
	configPath := writeDistributionsYAML(t, fmt.Sprintf(`
distributions:
  - id: proxmox
    name: Proxmox
    type: 6
    url_pattern: "/debian/(.+)$"
    host_pattern: "^download\\.proxmox\\.com(:\\d+)?$"
    benchmark_url: "pbs-client/dists/trixie/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "(InRelease|Release(\\.gpg)?)$"
        cache_control: "max-age=3600"
        rewrite: true
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "%s/debian/"
  - id: proxmox-pbs-client
    name: Proxmox Backup Client Alias
    type: 7
    url_pattern: "^/proxmox-pbs-client/(.+)$"
    benchmark_url: "dists/trixie/Release"
    cache_rules:
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "%s/debian/pbs-client/"
`, proxmox.URL, proxmox.URL))
	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(configPath); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	st := newTestState()
	st.SetMirror(distro.TypeDebian, debian.URL+"/debian/")
	st.SetMirror(distro.TypeUbuntu, debian.URL+"/ubuntu/")
	st.SetProxyMode(distro.TypeAllDistros)
	ps, err := NewPackageStruct(Options{
		State:    st,
		Registry: reg,
		CacheDir: t.TempDir(),
		Mode:     distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	type routeCase struct {
		name       string
		host       string
		path       string
		query      string
		originForm bool
		wantOrigin string
		wantPath   string
	}
	var cases []routeCase
	for _, suffix := range []string{
		"dists/trixie/InRelease",
		"dists/trixie/Release",
		"dists/trixie/Release.gpg",
		"dists/trixie/main/binary-amd64/Packages.xz",
		"dists/trixie/main/binary-amd64/Packages.gz",
		"dists/trixie/main/binary-amd64/by-hash/SHA256/" + strings.Repeat("a", 64),
		"pool/main/p/proxmox-backup-client/proxmox-backup-client_4.0.0-1_amd64.deb",
	} {
		for _, originForm := range []bool{false, true} {
			cases = append(cases, routeCase{
				name:       fmt.Sprintf("proxmox/%s/origin-form=%t", suffix, originForm),
				host:       "download.proxmox.com",
				path:       "/debian/pbs-client/" + suffix,
				originForm: originForm,
				wantOrigin: "proxmox",
				wantPath:   "/debian/pbs-client/" + suffix,
			})
		}
	}
	cases = append(cases,
		routeCase{
			name: "mixed case and explicit port", host: "Download.Proxmox.Com:80",
			path: "/debian/pbs-client/dists/trixie/Release", originForm: true,
			wantOrigin: "proxmox", wantPath: "/debian/pbs-client/dists/trixie/Release",
		},
		routeCase{
			name: "absolute request with explicit port", host: "download.proxmox.com:80",
			path:       "/debian/pbs-client/dists/trixie/Release",
			wantOrigin: "proxmox", wantPath: "/debian/pbs-client/dists/trixie/Release",
		},
		routeCase{
			name: "query preserved", host: "download.proxmox.com",
			path: "/debian/pbs-client/dists/trixie/Release", query: "?revision=1&source=apt",
			wantOrigin: "proxmox", wantPath: "/debian/pbs-client/dists/trixie/Release",
		},
		routeCase{
			name: "ordinary Debian", host: "deb.debian.org",
			path:       "/debian/dists/trixie/InRelease",
			wantOrigin: "debian", wantPath: "/debian/dists/trixie/InRelease",
		},
		routeCase{
			name: "unrelated host keeps generic path routing", host: "other.example",
			path:       "/debian/pbs-client/dists/trixie/Release",
			wantOrigin: "debian", wantPath: "/debian/pbs-client/dists/trixie/Release",
		},
		routeCase{
			name: "hostname suffix does not match", host: "download.proxmox.com.other.example",
			path:       "/debian/pbs-client/dists/trixie/Release",
			wantOrigin: "debian", wantPath: "/debian/pbs-client/dists/trixie/Release",
		},
		routeCase{
			name: "host alone does not override another distribution path", host: "download.proxmox.com",
			path:       "/ubuntu/dists/noble/InRelease",
			wantOrigin: "debian", wantPath: "/ubuntu/dists/noble/InRelease",
		},
		routeCase{
			name: "local path alias", host: "proxy.example:3142",
			path: "/proxmox-pbs-client/dists/trixie/InRelease", originForm: true,
			wantOrigin: "proxmox", wantPath: "/debian/pbs-client/dists/trixie/InRelease",
		},
		routeCase{
			name: "Debian security host-root fallback", host: "security.debian.org",
			path: "/dists/trixie-security/InRelease", originForm: true,
			wantOrigin: "debian", wantPath: "/debian-security/dists/trixie-security/InRelease",
		},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "http://" + tc.host + tc.path + tc.query
			if tc.originForm {
				target = tc.path + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			ps.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("X-Test-Origin"); got != tc.wantOrigin {
				t.Errorf("request reached %q origin, want %q", got, tc.wantOrigin)
			}
			upstream := debian.URL
			if tc.wantOrigin == "proxmox" {
				upstream = proxmox.URL
			}
			want := strings.TrimPrefix(upstream, "http://") + " " + tc.wantPath + tc.query + "\n"
			if got := rec.Body.String(); got != want {
				t.Errorf("upstream received %q, want %q", got, want)
			}
		})
	}
}

// Exercise the shipped example itself so its capture groups, mirror root and
// cache rules cannot drift from the routing behavior documented for users.
func TestProxmoxExampleRoutesOriginalAndAliasPaths(t *testing.T) {
	for _, trailingSlash := range []string{"/", ""} {
		t.Run("mirror suffix="+trailingSlash, func(t *testing.T) {
			debian := newRepositoryRoutingOrigin(t, "debian")
			proxmox := newRepositoryRoutingOrigin(t, "proxmox")
			reg := distro.NewBuiltinRegistry()
			if err := reg.Reload("../../examples/proxmox/distributions.yaml"); err != nil {
				t.Fatalf("Reload shipped example: %v", err)
			}
			registered, ok := reg.GetByID("proxmox-pbs-client")
			if !ok {
				t.Fatal("shipped example did not register proxmox-pbs-client")
			}
			// Replace only its origin before construction: mirror probes and routed
			// requests must stay local while all example routing fields stay intact.
			local := *registered
			local.Mirrors = []distro.URLWithAlias{
				distro.GenerateBuiltinMirrorItem(proxmox.URL+"/debian/pbs-client"+trailingSlash, true),
			}
			if err := reg.Register(&local); err != nil {
				t.Fatalf("register local example mirror: %v", err)
			}
			st := newTestState()
			st.SetMirror(distro.TypeDebian, debian.URL+"/debian/")
			ps, err := NewPackageStruct(Options{
				State: st, Registry: reg, CacheDir: t.TempDir(), Mode: distro.TypeAllDistros,
			})
			if err != nil {
				t.Fatalf("NewPackageStruct: %v", err)
			}
			for _, source := range []struct {
				name string
				base string
			}{
				{"original source", "http://download.proxmox.com/debian/pbs-client/"},
				{"local alias", "http://proxy.example:3142/proxmox-pbs-client/"},
			} {
				for _, suffix := range []string{
					"dists/trixie/Release",
					"dists/trixie/main/binary-amd64/Packages.xz",
					"pool/main/p/proxmox-backup-client/proxmox-backup-client_4.0.0-1_amd64.deb",
				} {
					t.Run(source.name+"/"+suffix, func(t *testing.T) {
						rec := httptest.NewRecorder()
						ps.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, source.base+suffix, nil))
						if rec.Code != http.StatusOK || rec.Header().Get("X-Test-Origin") != "proxmox" {
							t.Fatalf("status = %d, origin = %q; want 200 from proxmox", rec.Code, rec.Header().Get("X-Test-Origin"))
						}
						want := strings.TrimPrefix(proxmox.URL, "http://") + " /debian/pbs-client/" + suffix + "\n"
						if got := rec.Body.String(); got != want {
							t.Errorf("upstream received %q, want %q", got, want)
						}
					})
				}
			}

		})
	}
}

// Origins can host several archives. A host match alone must not pick the
// first one, and a selected archive's missing file rule must not allow a
// different archive to supply the response.
func TestExplicitHostPathSelectionKeepsRepositoryBoundaries(t *testing.T) {
	debian := newRepositoryRoutingOrigin(t, "debian")
	pbs := newRepositoryRoutingOrigin(t, "pbs")
	pve := newRepositoryRoutingOrigin(t, "pve")
	reg := distro.NewBuiltinRegistry()
	for _, cfg := range []distro.DistributionConfig{
		{
			ID: "a-pbs", Type: 6, URLPattern: `/debian/pbs-client/(.+)$`,
			HostPattern: `^download\.proxmox\.com$`, BenchmarkURL: "dists/trixie/InRelease",
			CacheRules: []distro.CacheRuleConfig{{Pattern: `InRelease$`, Rewrite: true}},
			Mirrors:    distro.MirrorListConfig{Official: []string{pbs.URL + "/debian/pbs-client/"}},
		},
		{
			ID: "z-pve", Type: 7, URLPattern: `/debian/pve/(.+)$`,
			HostPattern: `^download\.proxmox\.com$`, BenchmarkURL: "dists/trixie/Release",
			CacheRules: []distro.CacheRuleConfig{{Pattern: `(^|/)Release$`, Rewrite: true}},
			Mirrors:    distro.MirrorListConfig{Official: []string{pve.URL + "/debian/pve/"}},
		},
	} {
		if err := reg.LoadFromConfig(&cfg); err != nil {
			t.Fatalf("LoadFromConfig: %v", err)
		}
	}
	st := newTestState()
	st.SetMirror(distro.TypeDebian, debian.URL+"/debian/")
	ps, err := NewPackageStruct(Options{
		State: st, Registry: reg, CacheDir: t.TempDir(), Mode: distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	for _, tc := range []struct {
		name       string
		host       string
		path       string
		wantOrigin string
		wantStatus int
	}{
		{"first archive", "download.proxmox.com", "/debian/pbs-client/dists/trixie/InRelease", "pbs", http.StatusOK},
		{"second archive on same host", "download.proxmox.com", "/debian/pve/dists/trixie/Release", "pve", http.StatusOK},
		{"explicit archive missing file rule", "download.proxmox.com", "/debian/pbs-client/dists/trixie/Release", "", http.StatusNotFound},
		{"generic Debian still has Release rule", "deb.debian.org", "/debian/dists/trixie/Release", "debian", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ps.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+tc.host+tc.path, nil))
			if rec.Code != tc.wantStatus || rec.Header().Get("X-Test-Origin") != tc.wantOrigin {
				t.Fatalf("status = %d, origin = %q; want %d from %q", rec.Code, rec.Header().Get("X-Test-Origin"), tc.wantStatus, tc.wantOrigin)
			}
			if tc.wantStatus == http.StatusOK && !strings.HasSuffix(rec.Body.String(), " "+tc.path+"\n") {
				t.Errorf("upstream received %q, want path %q", rec.Body.String(), tc.path)
			}
		})
	}
}
