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
	"strings"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// A client that uses apt-proxy as its HTTP proxy names an origin in the
// request line (GET http://host/path). Distribution routing ignores that host
// on purpose: whichever mirror a client image points at, /ubuntu/... is served
// from the mirror apt-proxy selected. Two things still have to follow the
// named origin, and this file holds the checks for them: an origin the
// operator allowlisted, and the credentials a client sent for the origin.

// splitHostname separates an authority into its host and port. The host is
// lower-cased and loses IPv6 brackets and a trailing root dot.
func splitHostname(authority string) (host, port string) {
	host = authority
	if h, p, err := net.SplitHostPort(authority); err == nil {
		host, port = h, p
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.ToLower(host), port
}

// hostnameOf returns the host part of an authority as splitHostname does.
func hostnameOf(authority string) string {
	host, _ := splitHostname(authority)
	return host
}

// sameHost reports whether two authorities name the same host. Ports have to
// agree as well, except that the default ports of http and https are
// interchangeable: a client speaks http to the proxy while the upstream hop
// may be https.
func sameHost(a, b string) bool {
	hostA, portA := splitHostname(a)
	hostB, portB := splitHostname(b)
	if hostA == "" || hostA != hostB {
		return false
	}
	return portA == portB || (defaultWebPort(portA) && defaultWebPort(portB))
}

func defaultWebPort(port string) bool {
	return port == "" || port == "80" || port == "443"
}

// mirrorHostSet collects the hostnames of a distribution's listed mirrors.
// Entries from distributions.yaml may omit the scheme.
func mirrorHostSet(mirrors []distro.URLWithAlias) map[string]struct{} {
	if len(mirrors) == 0 {
		return nil
	}
	hosts := make(map[string]struct{}, len(mirrors))
	for _, mirror := range mirrors {
		authority := mirror.URL
		if _, rest, found := strings.Cut(authority, "://"); found {
			authority = rest
		}
		if end := strings.IndexAny(authority, "/?#"); end >= 0 {
			authority = authority[:end]
		}
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			authority = authority[at+1:]
		}
		if host := hostnameOf(authority); host != "" {
			hosts[host] = struct{}{}
		}
	}
	return hosts
}

// distributionHost reports whether host belongs to a configured distribution:
// a host pattern names it, or the distribution that claims path lists it as a
// mirror or currently fetches from it.
func distributionHost(rt *routing, host, path string) bool {
	name := hostnameOf(host)
	for _, entry := range rt.patterns {
		if entry.hostPattern != nil && entry.hostPattern.MatchString(host) {
			return true
		}
		if !matchesDistroPath(entry.pattern, path) {
			continue
		}
		if _, listed := entry.mirrorHosts[name]; listed {
			return true
		}
		if len(entry.rules) > 0 && rt.rewriters.fetchesFrom(entry.rules[0].OS, name) {
			return true
		}
	}
	return false
}

// withholdForeignCredentials removes the credentials a client sent for the
// origin it named when routing selected a different host.
//
// namedOrigin is the authority of an absolute-form request line and is empty
// for origin-form requests. Those address apt-proxy itself, so their
// credentials are meant for the repository behind it and keep reaching the
// configured upstream. Credentials sent with a named origin were issued for
// that origin: a distribution rewrite replaces it with the selected mirror,
// which must not receive them. The mirror's own configured credentials are
// applied afterwards as usual.
func (ap *PackageStruct) withholdForeignCredentials(r *http.Request, namedOrigin string) {
	if namedOrigin == "" || r.URL == nil || sameHost(namedOrigin, r.URL.Host) {
		return
	}
	if len(r.Header.Values("Authorization")) == 0 && len(r.Header.Values("Cookie")) == 0 {
		return
	}
	r.Header.Del("Authorization")
	r.Header.Del("Cookie")
	ap.log.Warn().
		Str("named_origin", namedOrigin).
		Str("upstream", r.URL.Host).
		Msg("client credentials for the named origin were not forwarded to a different upstream host")
}
