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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	tracing "github.com/soulteary/tracing-kit"
	"go.opentelemetry.io/otel/trace"

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

// stripDefaultPort drops a :80 or :443 that came along with the request and
// carries no meaning once the scheme is decided, and leaves anything else
// alone.
//
// It re-brackets an IPv6 literal on the way out. SplitHostPort returns the
// address unbracketed, and url.URL.Host without brackets reads the tail of the
// address as a port -- "[2001:db8::1]:443" would become "2001:db8::1", which
// serialises to https://2001:db8::1/... and dials port 1.
// stripRedundantTLSPort drops an explicit :443 from an origin a marker named.
// The marker always resolves to https, so :443 is that scheme's own default and
// carries no information; every other port -- 80 included -- names a TLS
// service deliberately listening there, and discarding it would send the
// request to 443 instead, silently reaching a different service.
//
// Deliberately narrower than stripDefaultPort, which serves the ordinary
// passthrough path: there the authority comes from the request line, where a
// :80 really is an http default that must go once the scheme is upgraded.
func stripRedundantTLSPort(authority string) string {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || port != "443" {
		return authority
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func stripDefaultPort(authority string) string {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || (port != "80" && port != "443") {
		return authority
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
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
		// A *default* port carried over from the http request line would dial
		// the wrong service once the scheme is upgraded, so drop it. Only when
		// the entry did not pin one: an entry written https://host:80 means a
		// TLS service deliberately listening there, and discarding that port
		// would send the request to 443 and never reach it.
		if rule.Port == "" {
			host = stripDefaultPort(authority)
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

// acceptTLSRewriteMarker decides what to do with a request carrying the
// apt-cacher-ng HTTPS/// marker, and points it at the origin when allowed.
//
// The marker is exactly a request to fetch an arbitrary origin over TLS, so it
// is gated by the same allowlist as ordinary passthrough. Returning a rule
// means the caller should serve the request; a nil return means the refusal
// has already been written.
func (ap *PackageStruct) acceptTLSRewriteMarker(
	rw http.ResponseWriter, r *http.Request, origin, upstreamPath string, span trace.Span,
) *distro.Rule {
	refuse := func(status int, msg string) *distro.Rule {
		ap.log.Warn().
			Str("origin", origin).
			Str("path", r.URL.Path).
			Int("status", status).
			Msg("refusing HTTPS/// rewrite marker")
		tracing.SetSpanAttributes(span, map[string]string{
			"http.status_code": strconv.Itoa(status),
		})
		http.Error(rw, msg, status)
		return nil
	}

	if origin == "" {
		return refuse(http.StatusBadRequest,
			"malformed apt-cacher-ng HTTPS/// marker: no origin named after the marker")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return refuse(http.StatusMethodNotAllowed,
			"apt-proxy only forwards GET and HEAD to third-party origins")
	}

	_, allowed := ap.passthrough.Match(origin)
	if !allowed {
		// Naming the origin and the setting turns a dead end into an
		// actionable message: the operator adds one entry and retries.
		return refuse(http.StatusForbidden, fmt.Sprintf(
			"%s is not in apt-proxy's passthrough allowlist; "+
				"add it (--passthrough=%s) to let apt-proxy fetch and cache it",
			origin, origin))
	}
	// The marker itself already says https, so rule.ForceHTTPS adds nothing
	// here -- and neither does rule.Port. The origin is a literal the client
	// typed inside the path, not an authority carried over from the request
	// line, so whatever port it names *is* the destination, whether or not the
	// allowlist entry happens to pin the same one.
	host := stripRedundantTLSPort(origin)

	// The marker's whole point is that the upstream is TLS.
	r.URL.Scheme = "https"
	r.URL.Host = host
	r.Host = host
	if decoded, err := url.PathUnescape(upstreamPath); err == nil {
		r.URL.Path = decoded
	} else {
		r.URL.Path = upstreamPath
	}
	if r.URL.Path != upstreamPath {
		r.URL.RawPath = upstreamPath
	} else {
		r.URL.RawPath = ""
	}

	ap.log.Debug().
		Str("origin", host).
		Str("path", r.URL.Path).
		Msg("resolving apt-cacher-ng HTTPS/// marker to its origin")

	return passthroughRule
}
