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
	"testing"
)

func TestGuardRequestPath(t *testing.T) {
	var calls int
	guarded := GuardRequestPath(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, path := range []string{
		"//admin",
		"/public/..%2Fadmin",
		"/public/..%2fadmin",
		"/repo/https-everywhere//package.deb",
		"/ubuntu/../debian/dists/stable/InRelease",
		"/ubuntu/./pool/main/a.deb",
		"/ubuntu/%2e%2e/secret",
		"/ubuntu/pool/..",
	} {
		before := calls
		// Over the wire: the request line reaches the server exactly as written.
		req, err := http.NewRequest(http.MethodGet, "http://proxy.example"+path, nil)
		if err != nil {
			t.Fatalf("%q: %v", path, err)
		}
		res := wireResponse(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusNoContent)
		}), req)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("over the wire %q: status = %d, want 404", path, res.StatusCode)
		}
		// In process, with an origin-form target.
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://proxy.example"+path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("in process %q: status = %d, want 404", path, rec.Code)
		}
		if calls != before {
			t.Errorf("ambiguous path %q reached the handler", path)
		}
	}

	for _, path := range []string{
		"/",
		"/ubuntu/dists/noble/",
		"/ubuntu/dists/noble/InRelease",
		"/debian/pool/main/g/gcc-12/libstdc%2b%2b6_12.2.0-14%2bdeb12u1_amd64.deb",
		"/debian/pool/main/o/openssl/openssl_3.0.15-1%7edeb12u1_amd64.deb",
		"/alpine/v3.22/main/x86_64/APKINDEX.tar.gz",
		"/ubuntu/pool/main/d/dots/a..b_1.0_all.deb",
	} {
		before := calls
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://proxy.example"+path, nil))
		if rec.Code != http.StatusNoContent || calls != before+1 {
			t.Errorf("ordinary path %q: status = %d, calls %d -> %d", path, rec.Code, before, calls)
		}
	}

	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, &http.Request{Method: http.MethodGet})
	if rec.Code != http.StatusNotFound {
		t.Errorf("request without URL: status = %d, want 404", rec.Code)
	}
}

// Every spelling of apt-cacher-ng's marker contains empty path segments. It
// must pass the guard, reach only the allowlisted origin, and keep its query.
func TestTLSRewriteMarkerOverTheWireReachesOnlyAllowlistedOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		origin string
	}{
		{"absolute", "http://HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease", "get.docker.com"},
		{"rewrite", "http://proxy:3142/HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease", "get.docker.com"},
		{"lowercase", "http://https///get.docker.com/linux/ubuntu/dists/jammy/InRelease", "get.docker.com"},
		{"explicit-port", "http://HTTPS:3142///get.docker.com:80/linux/ubuntu/dists/jammy/InRelease", "get.docker.com:80"},
		{"ipv6", "http://HTTPS///[2001:db8::1]/linux/ubuntu/dists/jammy/InRelease", "[2001:db8::1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps, seen := passthroughProxy(t, tc.origin)
			const query = "ticket=a%20b&proof=c%2Fd"
			req, err := http.NewRequest(http.MethodGet, tc.target+"?"+query, nil)
			if err != nil {
				t.Fatal(err)
			}
			res := wireResponse(t, ps, req)
			body, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if readErr != nil || res.StatusCode != http.StatusOK || string(body) != "InRelease" {
				t.Fatalf("response=%d/%q err=%v", res.StatusCode, body, readErr)
			}
			if seen.count != 1 || seen.scheme != "https" || seen.host != tc.origin || seen.path != "/linux/ubuntu/dists/jammy/InRelease" || seen.query != query {
				t.Fatalf("marker changed upstream selection: %+v", *seen)
			}
		})
	}
}
