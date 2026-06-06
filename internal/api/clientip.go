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

// Package api provides HTTP API handlers for apt-proxy management endpoints.
package api

import (
	"net"
	"net/http"
	"strings"
)

// ClientIPExtractor returns the "real" client IP for an incoming request,
// honouring X-Forwarded-For only when the immediate peer is a trusted proxy.
//
// This is shared between the auth and rate-limit middlewares so they cannot
// drift: if rate-limiting honours XFF for trusted proxies but auth logging
// records r.RemoteAddr, operators get inconsistent forensic trails.
//
// Construct one via NewClientIPExtractor and reuse it across handlers; the
// underlying CIDR list is parsed once at construction time.
type ClientIPExtractor struct {
	trustedProxies []*net.IPNet
}

// NewClientIPExtractor parses the given trusted-proxy CIDR list. Empty or
// malformed entries are skipped silently (callers that want diagnostics
// should validate beforehand). Pass nil/empty to disable XFF entirely
// (default secure behaviour).
func NewClientIPExtractor(trustedProxies []string) *ClientIPExtractor {
	e := &ClientIPExtractor{}
	for _, cidr := range trustedProxies {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		e.trustedProxies = append(e.trustedProxies, n)
	}
	return e
}

// AddTrustedProxy registers an already-parsed CIDR. Used by call sites that
// have validated the input themselves and want to report errors directly.
func (e *ClientIPExtractor) AddTrustedProxy(n *net.IPNet) {
	if n == nil {
		return
	}
	e.trustedProxies = append(e.trustedProxies, n)
}

// ClientIP returns the request's client IP. When the immediate peer
// (r.RemoteAddr) is in trustedProxies, the left-most syntactically valid
// entry of X-Forwarded-For is preferred; otherwise the peer's host part is
// used. Whitespace inside an XFF entry is rejected so attackers cannot
// smuggle a fake left-most identifier (e.g. "1.2.3.4 attacker").
func (e *ClientIPExtractor) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !e.isTrustedProxy(host) {
		return host
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	first := xff
	if i := strings.IndexByte(xff, ','); i >= 0 {
		first = xff[:i]
	}
	first = strings.TrimSpace(first)
	if first == "" || strings.ContainsAny(first, " \t") || net.ParseIP(first) == nil {
		return host
	}
	return first
}

func (e *ClientIPExtractor) isTrustedProxy(host string) bool {
	if len(e.trustedProxies) == 0 {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range e.trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
