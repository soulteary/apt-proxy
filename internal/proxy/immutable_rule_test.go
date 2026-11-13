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
	"reflect"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestImmutableRule(t *testing.T) {
	for _, tc := range []struct {
		cacheControl string
		want         bool
	}{
		{"max-age=100000, immutable", true},
		{"immutable", true},
		{"public, IMMUTABLE, max-age=60", true},
		{"max-age=60,Immutable", true},
		{"", false},
		{"max-age=3600", false},
		{"max-age=60, immutable-ish", false},
		{`max-age=60, ext="immutable"`, false},
	} {
		if got := immutableRule(&distro.Rule{CacheControl: tc.cacheControl}); got != tc.want {
			t.Errorf("immutableRule(%q) = %v, want %v", tc.cacheControl, got, tc.want)
		}
	}
	if immutableRule(nil) {
		t.Error("nil rule reported immutable")
	}
	if immutableRule(passthroughRule) {
		t.Error("passthrough rule reported immutable")
	}
}

func TestBuiltinImmutableRules(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for name, rules := range map[string][]distro.Rule{
		"ubuntu":       distro.UbuntuDefaultCacheRules,
		"ubuntu-ports": distro.UbuntuPortsDefaultCacheRules,
		"debian":       distro.DebianDefaultCacheRules,
	} {
		for path, want := range map[string]bool{
			"/pool/main/d/demo/demo_1.0_amd64.deb":                     true,
			"/pool/main/d/demo/demo-udeb_1.0_amd64.udeb":               true,
			"/pool/main/d/demo/demo_1.0.orig.tar.xz":                   true,
			"/dists/stable/main/binary-amd64/by-hash/SHA256/" + digest: true,
			"/dists/stable/InRelease":                                  false,
			"/dists/stable/Release.gpg":                                false,
			"/dists/stable/main/binary-amd64/Packages.xz":              false,
		} {
			rule, ok := MatchingRule(path, rules)
			if !ok || immutableRule(rule) != want {
				t.Errorf("%s %s: matched=%v immutable=%v, want %v", name, path, ok, immutableRule(rule), want)
			}
		}
	}
	for name, rules := range map[string][]distro.Rule{
		"centos": distro.CentosDefaultCacheRules,
		"alpine": distro.AlpineDefaultCacheRules,
	} {
		for i := range rules {
			if immutableRule(&rules[i]) {
				t.Errorf("%s rule %s reported immutable", name, rules[i].String())
			}
		}
	}
}

func TestDropZeroMaxAge(t *testing.T) {
	for _, tc := range []struct {
		name     string
		in, want http.Header
	}{
		{"max-age zero", http.Header{"Cache-Control": {"max-age=0"}}, http.Header{}},
		{"other directives kept", http.Header{"Cache-Control": {"max-age=0, only-if-cached"}}, http.Header{"Cache-Control": {"only-if-cached"}}},
		{"case and order", http.Header{"Cache-Control": {"no-store, MAX-AGE=0, No-Cache ,max-stale=60"}}, http.Header{"Cache-Control": {"no-store, No-Cache, max-stale=60"}}},
		{"repeated field lines", http.Header{"Cache-Control": {"max-age=0", "only-if-cached", "no-cache, min-fresh=5"}}, http.Header{"Cache-Control": {"only-if-cached", "no-cache, min-fresh=5"}}},
		{"zero spellings", http.Header{"Cache-Control": {`max-age="0", max-age=00, max-age = 0, no-store`}}, http.Header{"Cache-Control": {"no-store"}}},
		{"quoted comma", http.Header{"Cache-Control": {`ext="a, max-age=0, b", max-age=0`}}, http.Header{"Cache-Control": {`ext="a, max-age=0, b"`}}},
		{"empty elements", http.Header{"Cache-Control": {"max-age=0,, only-if-cached,"}}, http.Header{"Cache-Control": {"only-if-cached"}}},
		{"raw field name", http.Header{"cache-control": {"max-age=0"}, "pragma": {"No-Cache"}}, http.Header{"pragma": {"No-Cache"}}},
		// An explicit request for a copy from the origin is not a routine hint.
		{"no-cache with pragma", http.Header{"Cache-Control": {"no-cache"}, "Pragma": {"no-cache"}}, http.Header{"Cache-Control": {"no-cache"}, "Pragma": {"no-cache"}}},
		{"pragma is not Cache-Control", http.Header{"Pragma": {"max-age=0"}}, http.Header{"Pragma": {"max-age=0"}}},
		// Nothing to remove: the field keeps its exact spelling.
		{"positive lifetime", http.Header{"Cache-Control": {"max-age=60,only-if-cached"}}, http.Header{"Cache-Control": {"max-age=60,only-if-cached"}}},
		{"malformed lifetime", http.Header{"Cache-Control": {"max-age, max-age=, max-age=soon, max-age=-0"}}, http.Header{"Cache-Control": {"max-age, max-age=, max-age=soon, max-age=-0"}}},
		{"other fields", http.Header{"If-Modified-Since": {immutableTestModified}, "Range": {"bytes=0-1"}}, http.Header{"If-Modified-Since": {immutableTestModified}, "Range": {"bytes=0-1"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Clone()
			dropZeroMaxAge(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	dropZeroMaxAge(nil)
}

func TestServeMatchedRepositoryNormalizesImmutableRequests(t *testing.T) {
	immutable := &distro.Rule{CacheControl: "max-age=100000, immutable"}
	mutable := &distro.Rule{CacheControl: "max-age=3600"}
	for _, tc := range []struct {
		name               string
		rule               *distro.Rule
		private            string
		cacheControl, want string
	}{
		{name: "immutable", rule: immutable, cacheControl: "max-age=0", want: ""},
		{name: "immutable keeps cache-only", rule: immutable, cacheControl: "max-age=0, only-if-cached", want: "only-if-cached"},
		{name: "immutable keeps an explicit bypass", rule: immutable, cacheControl: "no-cache, no-store", want: "no-cache, no-store"},
		{name: "mutable", rule: mutable, cacheControl: "max-age=0", want: "max-age=0"},
		{name: "passthrough", rule: passthroughRule, cacheControl: "max-age=0", want: "max-age=0"},
		{name: "authorized", rule: immutable, private: "Authorization", cacheControl: "max-age=0", want: "no-store, max-age=0"},
		{name: "cookie", rule: immutable, private: "Cookie", cacheControl: "max-age=0", want: "no-store, max-age=0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got *http.Request
			ps := &PackageStruct{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r })}
			req := httptest.NewRequest(http.MethodGet, "http://mirror.example/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb", nil)
			req.Header.Set("Cache-Control", tc.cacheControl)
			req.Header.Set("Pragma", "no-cache")
			req.Header.Set("If-Modified-Since", immutableTestModified)
			if tc.private != "" {
				req.Header.Set(tc.private, "secret")
			}
			ps.serveMatchedRepository(httptest.NewRecorder(), req, tc.rule)
			if got == nil {
				t.Fatal("request did not reach the handler")
			}
			if cc := strings.Join(got.Header.Values("Cache-Control"), "|"); cc != tc.want {
				t.Errorf("Cache-Control = %q, want %q", cc, tc.want)
			}
			if got.Header.Get("Pragma") != "no-cache" {
				t.Errorf("Pragma changed: %v", got.Header)
			}
			if got.Header.Get("If-Modified-Since") != immutableTestModified {
				t.Errorf("conditional header changed: %v", got.Header)
			}
			if rule, _ := got.Context().Value(cacheRuleKey{}).(*distro.Rule); rule != tc.rule {
				t.Errorf("matched rule not passed on: %v", rule)
			}
		})
	}
}
