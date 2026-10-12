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
	"context"
	"net/http"
	"strings"

	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func privateRepositoryRequest(r *http.Request) bool {
	return len(r.Header.Values("Authorization")) > 0 || len(r.Header.Values("Cookie")) > 0 || r.URL.User != nil
}

// Convert URL credentials before cache lookup and logging. Explicit request
// Authorization retains precedence, matching net/http's transport behavior.
func normalizeRepositoryCredentials(r *http.Request) {
	if r.URL.User == nil {
		return
	}
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	if r.Header.Get("Authorization") == "" {
		password, _ := r.URL.User.Password()
		r.SetBasicAuth(r.URL.User.Username(), password)
	}
	r.URL.User = nil
}
func addNoStore(h http.Header) {
	// Keep the bypass directive in the first field value. Some cache consumers
	// read Header.Get instead of combining repeated Cache-Control fields.
	directives := strings.Join(h.Values("Cache-Control"), ", ")
	if directives == "" {
		directives = "no-store"
	} else {
		directives = "no-store, " + directives
	}
	h.Set("Cache-Control", directives)
}

func hasCacheDirective(cc httpcache.CacheControl, name string) bool {
	for directive := range cc {
		if strings.EqualFold(directive, name) {
			return true
		}
	}
	return false
}

func protectPrivateRepositoryResponse(res *http.Response) {
	if privateRepositoryRequest(res.Request) || len(res.Header.Values("Set-Cookie")) > 0 {
		addNoStore(res.Header)
	}
}

// Authentication and cookies are deliberately outside apt-proxy's shared
// cache, even when the upstream declares a response public. Cache-only requests
// must fail without contacting upstream rather than falling through bypass.
func (ap *PackageStruct) serveMatchedRepository(w http.ResponseWriter, r *http.Request, rule *distro.Rule) {
	normalizeRepositoryCredentials(r)
	if privateRepositoryRequest(r) {
		cc, err := httpcache.ParseCacheControlHeaders(r.Header)
		if err != nil {
			http.Error(w, "invalid Cache-Control", http.StatusBadRequest)
			return
		}
		if hasCacheDirective(cc, "only-if-cached") {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "authenticated or cookie-bearing requests cannot use the shared cache", http.StatusGatewayTimeout)
			return
		}
		addNoStore(r.Header)
	}
	ap.Handler.ServeHTTP(&responseWriter{w, rule}, r.WithContext(context.WithValue(r.Context(), cacheRuleKey{}, rule)))
}
