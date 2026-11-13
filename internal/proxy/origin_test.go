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
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// testMirrorHost is the host of every mirror newTestState selects.
const testMirrorHost = "mirrors.example.com"

// absoluteRequest is the request a client sends when apt-proxy is its HTTP
// proxy, as net/http hands it to a handler: the request line names the origin,
// so both URL.Host and Host carry it.
func absoluteRequest(origin, path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://"+origin+path, nil)
}

// serveRecorded sends r through ps and returns the response status.
func serveRecorded(ps *PackageStruct, r *http.Request) int {
	rec := httptest.NewRecorder()
	ps.ServeHTTP(rec, r)
	return rec.Code
}

// A third-party archive whose path starts like a distribution's must reach the
// allowlisted origin. Path-only distribution routing used to claim it first.
func TestAllowlistedOriginIsNotRoutedByPath(t *testing.T) {
	for _, tc := range []struct{ origin, path string }{
		{"packages.microsoft.com", "/ubuntu/22.04/prod/dists/jammy/InRelease"},
		{"repo.jellyfin.org", "/ubuntu/dists/jammy/InRelease"},
		{"repo.jellyfin.org", "/debian/pool/main/j/jellyfin/jellyfin_10.9.0_all.deb"},
		{"dl.yarnpkg.com", "/debian/dists/stable/InRelease"},
	} {
		for name, request := range map[string]*http.Request{
			"absolute form": absoluteRequest(tc.origin, tc.path),
			"host header":   proxyModeRequest(http.MethodGet, tc.origin, tc.path),
		} {
			t.Run(tc.origin+tc.path+"/"+name, func(t *testing.T) {
				ps, seen := passthroughProxy(t, tc.origin)
				if status := serveRecorded(ps, request); status != http.StatusOK {
					t.Fatalf("status = %d, want 200", status)
				}
				if seen.count != 1 || seen.host != tc.origin || seen.path != tc.path {
					t.Fatalf("upstream saw %d request(s) for %s%s, want one for %s%s",
						seen.count, seen.host, seen.path, tc.origin, tc.path)
				}
			})
		}
	}
}

// Without an allowlist entry the host is still ignored: a client image may
// name any mirror of the distribution, and the selected mirror serves it.
func TestUnlistedOriginKeepsPathRouting(t *testing.T) {
	ps, seen := passthroughProxy(t, "ppa.launchpad.net")

	const path = "/ubuntu/dists/jammy/InRelease"
	if status := serveRecorded(ps, absoluteRequest("mirror.unlisted.example", path)); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if seen.host != testMirrorHost || seen.path != path {
		t.Fatalf("upstream saw %s%s, want the selected mirror %s%s", seen.host, seen.path, testMirrorHost, path)
	}
}

// An allowlisted host that belongs to the distribution keeps its routing and
// cache rules for the distribution's paths, and passes through for the rest.
func TestAllowlistedDistributionHostKeepsItsRouting(t *testing.T) {
	ubuntu, ok := newTestRegistry().GetByType(distro.TypeUbuntu)
	if !ok || len(ubuntu.Mirrors) == 0 {
		t.Fatal("the built-in registry lists no Ubuntu mirror")
	}
	listed, err := url.Parse(ubuntu.Mirrors[0].URL)
	if err != nil || listed.Host == "" {
		t.Fatalf("unusable listed mirror %q: %v", ubuntu.Mirrors[0].URL, err)
	}

	for name, host := range map[string]string{
		"listed mirror":   listed.Host,
		"selected mirror": testMirrorHost,
	} {
		t.Run(name, func(t *testing.T) {
			ps, seen := passthroughProxy(t, host)

			r := absoluteRequest(host, "/ubuntu/dists/jammy/InRelease")
			rule := ps.handleExternalURLs(r)
			if rule == nil || rule.OS != distro.TypeUbuntu {
				t.Fatalf("rule = %v, want the Ubuntu rule", rule)
			}
			if r.URL.Host != testMirrorHost {
				t.Errorf("request went to %s, want the selected mirror", r.URL.Host)
			}

			// A path outside the distribution on the same host is third-party.
			const other = "/docker-ce/linux/ubuntu/dists/jammy/InRelease"
			if status := serveRecorded(ps, absoluteRequest(host, other)); status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			if seen.host != host || seen.path != other {
				t.Errorf("upstream saw %s%s, want %s%s", seen.host, seen.path, host, other)
			}
		})
	}
}

func TestMirrorHostSet(t *testing.T) {
	hosts := mirrorHostSet([]distro.URLWithAlias{
		{URL: "https://Mirrors.Example.org/ubuntu/"},
		{URL: "http://user:secret@private.example.net:8080/ubuntu/"},
		{URL: "plain.example.com/ubuntu/"},
		{URL: "http:///no-host/"},
	})
	for _, want := range []string{"mirrors.example.org", "private.example.net", "plain.example.com"} {
		if _, ok := hosts[want]; !ok {
			t.Errorf("missing %q in %v", want, hosts)
		}
	}
	if len(hosts) != 3 {
		t.Errorf("hosts = %v, want three entries", hosts)
	}
	if mirrorHostSet(nil) != nil {
		t.Error("an empty mirror list must yield no set")
	}
}
