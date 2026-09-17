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

// Package passthrough holds the allowlist of third-party origins apt-proxy
// will fetch and cache on a client's behalf.
//
// apt-proxy mirrors distributions: it rewrites a request onto a mirror it was
// configured for, and anything else is a 404. That is deliberate -- it is not
// an open forward proxy. But the archives people actually install from are not
// only distributions: a Launchpad PPA, a vendor repository like
// download.docker.com, an internal archive. Without a way to name those, each
// one has to be modelled as a distribution in distributions.yaml, or skipped
// with a per-host DIRECT rule on every client.
//
// An allowlist is the middle ground, and it is the same shape apt-cacher-ng
// settles on with PassThroughPattern: the operator names the origins, and only
// those are fetched unrewritten. The list is the security boundary, so parsing
// is strict -- an entry that is not plainly a public origin is rejected at
// startup rather than tolerated at request time.
package passthrough

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Rule is one allowlisted origin.
type Rule struct {
	// Host is the lower-cased hostname, without a port.
	Host string
	// Port, when set, is the only port this rule matches. Empty means the
	// scheme default (80 or 443) -- an allowlisted archive host should not
	// also expose whatever else happens to listen on that machine.
	Port string
	// ForceHTTPS upgrades the upstream request, for an origin written as
	// https://host. apt speaks http to its proxy, so without this an
	// https-only archive answers with a redirect the client cannot follow
	// back through apt-proxy.
	ForceHTTPS bool
}

// List is a parsed allowlist. The zero value allows nothing, which is what an
// unconfigured apt-proxy must do.
type List struct {
	rules []Rule
}

// Empty reports whether the list allows nothing.
func (l *List) Empty() bool { return l == nil || len(l.rules) == 0 }

// Rules returns the parsed entries, for logging and tests.
func (l *List) Rules() []Rule {
	if l == nil {
		return nil
	}
	return append([]Rule(nil), l.rules...)
}

// Match reports the rule covering authority, which may carry a port.
func (l *List) Match(authority string) (Rule, bool) {
	if l.Empty() || authority == "" {
		return Rule{}, false
	}

	host, port := splitAuthority(strings.ToLower(authority))
	if host == "" {
		return Rule{}, false
	}

	for _, rule := range l.rules {
		if rule.Host != host {
			continue
		}
		if rule.Port != "" {
			if port == rule.Port {
				return rule, true
			}
			continue
		}
		// No port on the entry: default ports only.
		if port == "" || port == "80" || port == "443" {
			return rule, true
		}
	}
	return Rule{}, false
}

// Parse builds a List from configuration entries. Accepted forms are a bare
// host ("ppa.launchpad.net", "repo.example.com:8080") and a host with a
// scheme, where https:// additionally forces the upstream request to TLS.
//
// Everything else is an error. A silently-dropped entry in an allowlist reads
// as "allowed" to whoever wrote it, so a typo has to be loud.
func Parse(entries []string) (*List, error) {
	list := &List{}
	seen := make(map[string]struct{}, len(entries))

	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		rule, err := parseEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("passthrough entry %q: %w", entry, err)
		}

		key := rule.Host + ":" + rule.Port
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		list.rules = append(list.rules, rule)
	}
	return list, nil
}

func parseEntry(entry string) (Rule, error) {
	var rule Rule

	authority := entry
	switch {
	case strings.HasPrefix(strings.ToLower(entry), "https://"):
		rule.ForceHTTPS = true
		authority = entry[len("https://"):]
	case strings.HasPrefix(strings.ToLower(entry), "http://"):
		authority = entry[len("http://"):]
	case strings.Contains(entry, "://"):
		return rule, fmt.Errorf("only http:// and https:// origins are supported")
	}

	// A trailing slash is a natural thing to paste; anything beyond it is a
	// path, which this list does not express.
	authority = strings.TrimSuffix(authority, "/")
	if strings.ContainsAny(authority, "/?#") {
		return rule, fmt.Errorf("name an origin only, without a path")
	}
	if strings.ContainsAny(authority, "*") {
		return rule, fmt.Errorf("wildcards are not supported; name each origin")
	}
	if strings.ContainsAny(authority, "@ \t") {
		return rule, fmt.Errorf("must be a bare host[:port]")
	}

	host, port := splitAuthority(strings.ToLower(authority))
	if host == "" {
		return rule, fmt.Errorf("missing host")
	}
	if port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return rule, fmt.Errorf("invalid port %q", port)
		}
	}
	if err := rejectNonPublicHost(host); err != nil {
		return rule, err
	}
	// url.Parse is the last word on whether this is a usable authority.
	if _, err := url.Parse("http://" + authority); err != nil {
		return rule, fmt.Errorf("not a valid origin: %w", err)
	}

	rule.Host = host
	rule.Port = port
	return rule, nil
}

// rejectNonPublicHost keeps the allowlist from being pointed back at the host
// running apt-proxy or at the network behind it. apt-proxy answers unauthenticated
// clients, so an entry naming a loopback or private address turns it into a
// reachable hop into that network.
//
// This is a guard against a misconfigured allowlist, not a general SSRF
// defence: a public name can still resolve wherever its DNS says. The
// allowlist itself remains the boundary.
func rejectNonPublicHost(host string) error {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("refusing to allow %q; passthrough is for public archives", host)
	}

	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return nil
	}
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(), ip.IsUnspecified(), ip.IsInterfaceLocalMulticast():
		return fmt.Errorf("refusing to allow non-public address %q", host)
	}
	return nil
}

// splitAuthority separates host and port, tolerating an authority with no port
// and IPv6 literals in brackets.
func splitAuthority(authority string) (host, port string) {
	if h, p, err := net.SplitHostPort(authority); err == nil {
		return strings.Trim(h, "[]"), p
	}
	return strings.Trim(authority, "[]"), ""
}
