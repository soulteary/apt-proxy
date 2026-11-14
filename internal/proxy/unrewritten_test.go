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
	"regexp"
	"strings"
	"testing"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// modeProxy builds a PackageStruct for one mode on the mirrors of
// newTestState, with a transport that records instead of dialling.
func modeProxy(t *testing.T, mode int) (*PackageStruct, *recordedRequest) {
	t.Helper()
	st := newTestState()
	st.SetProxyMode(mode)
	seen := &recordedRequest{}
	ps, err := NewPackageStruct(Options{
		State: st, Registry: newTestRegistry(), CacheDir: t.TempDir(), Logger: logger.Default(), Mode: mode,
		TransportOverride: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			seen.count++
			seen.host, seen.path = r.URL.Host, r.URL.EscapedPath()
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader("upstream")), Request: r,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}
	return ps, seen
}

// A distribution request is fetched from the mirror apt-proxy selected and
// from nowhere else. When it cannot be pointed at a mirror, the only upstream
// left is the authority the client wrote into the request, which may be any
// host the proxy can reach. Such a request is not served.
func TestDistributionRequestIsFetchedOnlyFromItsMirror(t *testing.T) {
	// The host a client names. It is not a mirror of anything.
	const named = "internal.example"

	for _, tc := range []struct {
		name string
		mode int
		path string
		// wantPath is the upstream path at the mirror; empty means the
		// request must be answered with 404 without any upstream request.
		wantPath string
	}{
		{"rewritten in the default mode", distro.TypeAllDistros, "/ubuntu/dists/x/InRelease", "/ubuntu/dists/x/InRelease"},
		{"escape outside the distribution segment", distro.TypeAllDistros, "/ubuntu/dists/x/InReleas%65", "/ubuntu/dists/x/InReleas%65"},
		{"escaped distribution segment, ubuntu", distro.TypeAllDistros, "/%75buntu/dists/x/InRelease", ""},
		{"escaped distribution segment, debian", distro.TypeAllDistros, "/%64ebian/dists/x/InRelease", ""},
		{"escaped distribution segment, alpine", distro.TypeAllDistros, "/%61lpine/v3.22/main/x86_64/APKINDEX.tar.gz", ""},
		{"escaped distribution segment, centos", distro.TypeAllDistros, "/cento%73/9-stream/BaseOS/x86_64/os/repodata/repomd.xml", ""},
		{"selected distribution", distro.TypeDebian, "/debian/dists/x/InRelease", "/debian/dists/x/InRelease"},
		{"distribution the mode does not select, ubuntu", distro.TypeDebian, "/ubuntu/dists/x/InRelease", ""},
		{"distribution the mode does not select, ubuntu-ports", distro.TypeDebian, "/ubuntu-ports/dists/x/InRelease", ""},
		{"distribution the mode does not select, alpine", distro.TypeDebian, "/alpine/v3.22/main/x86_64/APKINDEX.tar.gz", ""},
		{"distribution the mode does not select, centos", distro.TypeDebian, "/centos/9-stream/BaseOS/x86_64/os/repodata/repomd.xml", ""},
		{"distribution the mode does not select, debian", distro.TypeUbuntu, "/debian/dists/x/InRelease", ""},
	} {
		for form, request := range map[string]*http.Request{
			// A proxy client: the request line carries the authority.
			"absolute form": httptest.NewRequest(http.MethodGet, "http://"+named+tc.path, nil),
			// A redirected or direct client: only Host carries it.
			"origin form": proxyModeRequest(http.MethodGet, named, tc.path),
		} {
			t.Run(tc.name+"/"+form, func(t *testing.T) {
				ps, seen := modeProxy(t, tc.mode)
				rec := httptest.NewRecorder()
				ps.ServeHTTP(rec, request)

				if tc.wantPath == "" {
					if rec.Code != http.StatusNotFound || seen.count != 0 {
						t.Fatalf("status = %d with %d upstream request(s) to %q, want 404 and none", rec.Code, seen.count, seen.host)
					}
					return
				}
				if rec.Code != http.StatusOK || seen.count != 1 {
					t.Fatalf("status = %d with %d upstream request(s), want 200 and one", rec.Code, seen.count)
				}
				if seen.host != testMirrorHost || seen.path != tc.wantPath {
					t.Fatalf("upstream request went to %s%s, want %s%s", seen.host, seen.path, testMirrorHost, tc.wantPath)
				}
			})
		}
	}
}

// A rule with rewrite disabled names no upstream either.
func TestRuleWithoutRewriteIsNotServed(t *testing.T) {
	ps, _ := modeProxy(t, distro.TypeAllDistros)
	entry := hostPatternEntry{
		repositoryID: "plain",
		pattern:      regexp.MustCompile(`/plain/(.+)$`),
		rules:        []distro.Rule{{OS: distro.TypeUbuntu, Pattern: regexp.MustCompile(`.*`), CacheControl: "max-age=60", Rewrite: false}},
	}
	r := httptest.NewRequest(http.MethodGet, "http://internal.example/plain/file", nil)
	if rule := ps.processMatchingRule(r, ps.currentRouting(), entry); rule != nil {
		t.Fatalf("rule %v returned for a request without an upstream", rule)
	}
	if r.URL.Host != "internal.example" {
		t.Fatalf("refused request was modified: %s", r.URL)
	}
}

func TestRewriteToMirrorReportsWhetherItRewrote(t *testing.T) {
	st := newTestState()
	rewriters := newRewriters(distro.ModeOf(distro.TypeUbuntu), st, newTestRegistry(), false, nil)
	for _, tc := range []struct {
		name string
		mode int
		url  string
		want bool
	}{
		{"matching path", distro.TypeUbuntu, "http://any.example/ubuntu/dists/x/InRelease", true},
		{"no rewriter for the type", distro.TypeDebian, "http://any.example/debian/dists/x/InRelease", false},
		{"path of another distribution", distro.TypeUbuntu, "http://any.example/debian/dists/x/InRelease", false},
		{"escaped distribution segment", distro.TypeUbuntu, "http://any.example/%75buntu/dists/x/InRelease", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if got := rewriteToMirror(r, rewriters, tc.mode); got != tc.want {
				t.Fatalf("rewriteToMirror = %v, want %v", got, tc.want)
			}
			if rewritten := r.URL.Host == testMirrorHost; rewritten != tc.want {
				t.Fatalf("request host is %s", r.URL.Host)
			}
		})
	}
	if rewriteToMirror(httptest.NewRequest(http.MethodGet, "http://any.example/ubuntu/x", nil), nil, distro.TypeUbuntu) {
		t.Fatal("rewrite reported without rewriters")
	}
}
