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

// Package access provides package-download client access controls.
package access

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
)

// Clients is an immutable connection-peer CIDR policy.
type Clients struct{ allow, deny []netip.Prefix }

// NewClients rejects malformed policy entries instead of silently opening access.
func NewClients(allow, deny []string) (*Clients, error) {
	p := &Clients{}
	for _, list := range []struct {
		name   string
		input  []string
		target *[]netip.Prefix
	}{
		{"download_allow_cidrs", allow, &p.allow}, {"download_deny_cidrs", deny, &p.deny}} {
		for _, value := range list.input {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, fmt.Errorf("%s: invalid CIDR %q: %w", list.name, value, err)
			}
			if prefix.Addr().Is4In6() {
				return nil, fmt.Errorf("%s: use an IPv4 CIDR instead of %q", list.name, value)
			}
			*list.target = append(*list.target, prefix.Masked())
		}
	}
	return p, nil
}
func contains(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Allowed uses the immediate connection peer; deny takes precedence.
func (p *Clients) Allowed(remote string) bool {
	if len(p.allow) == 0 && len(p.deny) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap().WithZone("")
	return !contains(p.deny, ip) && (len(p.allow) == 0 || contains(p.allow, ip))
}

// Wrap checks access before routing, cache lookup or upstream access.
// Forwarded headers are deliberately not credentials for download access.
func (p *Clients) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.Allowed(r.RemoteAddr) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "package download access denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
