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
	"strings"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// A client that uses apt-proxy as its HTTP proxy names an origin in the
// request line (GET http://host/path). Distribution routing ignores that host
// on purpose: whichever mirror a client image points at, /ubuntu/... is served
// from the mirror apt-proxy selected. An origin the operator allowlisted still
// has to be fetched from that origin, and this file holds the check for it.

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
