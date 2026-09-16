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
	"net"
	"net/http"
	"regexp"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// passthroughPattern exists only to satisfy distro.Rule, whose Pattern is
// dereferenced by String(). Passthrough never routes by path.
var passthroughPattern = regexp.MustCompile(`.*`)

// passthroughRule is the rule returned for an allowlisted origin. CacheControl
// is empty on purpose: apt-proxy knows what a distribution's index and package
// TTLs should be, but it knows nothing about a third-party archive, so the
// origin's own Cache-Control decides and httpcache applies it.
var passthroughRule = &distro.Rule{
	OS:           distro.TypeAllDistros,
	Pattern:      passthroughPattern,
	CacheControl: "",
	Rewrite:      false,
}

// matchPassthrough reports whether r addresses an allowlisted third-party
// origin, and if so points the request at it.
//
// This runs only after distribution matching has failed, so an archive
// apt-proxy mirrors keeps its existing routing and its own cache rules; the
// allowlist covers what would otherwise be a 404.
//
// The origin is taken from the request authority rather than the path: a
// client reaches a third-party archive by using apt-proxy as its HTTP proxy
// (http_proxy=...), which addresses the origin in the request line. In the
// URL-prefix form the authority is apt-proxy itself, which is not on the
// allowlist, so nothing matches and the request still 404s.
func (ap *PackageStruct) matchPassthrough(r *http.Request) *distro.Rule {
	if ap == nil || ap.passthrough.Empty() || r == nil || r.URL == nil {
		return nil
	}
	// apt fetches; it does not write. Anything else is not a repository
	// access and has no business leaving through the allowlist.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil
	}

	authority := requestHost(r)
	rule, ok := ap.passthrough.Match(authority)
	if !ok {
		return nil
	}

	// Fiber's adaptor hands us a server request: the authority is in Host and
	// URL.Host is empty. The ReverseProxy needs an absolute URL, so fill it in
	// from what the client addressed. The path is left exactly as sent.
	scheme, host := "http", authority
	if rule.ForceHTTPS {
		scheme = "https"
		// A default port carried over from an http:// request line would
		// dial the wrong service once the scheme is upgraded.
		if h, p, err := net.SplitHostPort(authority); err == nil && (p == "80" || p == "443") {
			host = h
		}
	}
	r.URL.Scheme = scheme
	r.URL.Host = host
	r.Host = host

	ap.log.Debug().
		Str("origin", host).
		Str("scheme", scheme).
		Str("path", r.URL.Path).
		Msg("passing request through to allowlisted origin")

	return passthroughRule
}
