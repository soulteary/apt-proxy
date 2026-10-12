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

package access

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUpstreamDenied identifies a permanent policy rejection, not a retryable network failure.
var ErrUpstreamDenied = errors.New("upstream access denied")

func denied(reason string) error { return fmt.Errorf("%w: %s", ErrUpstreamDenied, reason) }

// UpstreamConfig configures an optional direct package-download connection policy.
type UpstreamConfig struct {
	Enabled      bool     `yaml:"enabled"`
	AllowedHosts []string `yaml:"allowed_hosts"`
	AllowedPorts []int    `yaml:"allowed_ports"`
	AllowCIDRs   []string `yaml:"allow_cidrs"`
}

// Upstream checks request authorities and pins new connections to approved IPs.
// Its policy is immutable once constructed.
type Upstream struct {
	hosts         map[string]bool
	ports         map[int]bool
	exceptions    []netip.Prefix
	lookup        func(context.Context, string, string) ([]netip.Addr, error)
	dial          func(context.Context, string, string) (net.Conn, error)
	dialTimeout   time.Duration
	fallbackDelay time.Duration
}

var hostName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

func canonicalHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", fmt.Errorf("upstream host cannot contain a zone")
		}
		return ip.Unmap().String(), nil
	}
	if len(host) > 253 || !hostName.MatchString(host) {
		return "", fmt.Errorf("invalid upstream host")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("invalid upstream host")
		}
	}
	return host, nil
}

// NewUpstream validates even disabled policies. Nil leaves existing behavior intact.
func NewUpstream(cfg *UpstreamConfig) (*Upstream, error) {
	if cfg == nil {
		return nil, nil
	}
	p := &Upstream{dialTimeout: 30 * time.Second, fallbackDelay: 300 * time.Millisecond, hosts: map[string]bool{}, ports: map[int]bool{}, lookup: net.DefaultResolver.LookupNetIP, dial: (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext}
	for _, host := range cfg.AllowedHosts {
		normalized, err := canonicalHost(host)
		if err != nil {
			return nil, fmt.Errorf("upstream_access.allowed_hosts: %q: %w", host, err)
		}
		p.hosts[normalized] = true
	}
	for _, port := range cfg.AllowedPorts {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("upstream_access.allowed_ports: invalid port %d", port)
		}
		p.ports[port] = true
	}
	if len(p.ports) == 0 {
		p.ports[80] = true
		p.ports[443] = true
	}
	for _, cidr := range cfg.AllowCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("upstream_access.allow_cidrs: invalid CIDR %q", cidr)
		}
		p.exceptions = append(p.exceptions, prefix.Masked())
	}
	if !cfg.Enabled {
		if len(cfg.AllowedHosts)+len(cfg.AllowedPorts)+len(cfg.AllowCIDRs) > 0 {
			return nil, fmt.Errorf("upstream_access: restrictions require enabled: true")
		}
		return nil, nil
	}
	return p, nil
}

func (p *Upstream) authority(host, port string) error {
	normalized, err := canonicalHost(host)
	if err != nil {
		return denied(err.Error())
	}
	if len(p.hosts) > 0 && !p.hosts[normalized] {
		return denied("upstream host is not allowed")
	}
	number, err := strconv.Atoi(port)
	if err != nil || !p.ports[number] {
		return denied("upstream port is not allowed")
	}
	return nil
}

// CheckURL runs on every request, including requests reusing pooled connections.
func (p *Upstream) CheckURL(u *url.URL) error {
	if u == nil || u.Opaque != "" || u.Host == "" {
		return denied("invalid upstream URL")
	}
	port := u.Port()
	switch u.Scheme {
	case "http":
		if port == "" {
			port = "80"
		}
	case "https":
		if port == "" {
			port = "443"
		}
	default:
		return denied("upstream scheme is not allowed")
	}
	return p.authority(u.Hostname(), port)
}

// These include special-purpose ranges which IsGlobalUnicast considers unicast,
// plus transition networks whose destination may translate to an unchecked IPv4.
var specialNetworks = func() []netip.Prefix {
	var result []netip.Prefix
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "fec0::/10", "2001:db8::/32", "2002::/16"} {
		result = append(result, netip.MustParsePrefix(cidr))
	}
	return result
}()

func (p *Upstream) addressAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if contains(p.exceptions, ip) {
		return true
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !contains(specialNetworks, ip)
}

// DialContext validates every DNS answer before dialing a numeric address. It
// never passes the hostname to a second resolver; mixed safe/unsafe DNS fails.
func (p *Upstream) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, denied("invalid upstream authority")
	}
	if err := p.authority(host, port); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.dialTimeout)
	defer cancel()
	var addresses []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = p.lookup(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("upstream DNS lookup failed: %w", err)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("upstream DNS returned no addresses")
	}
	for _, ip := range addresses {
		if !p.addressAllowed(ip) {
			return nil, denied("upstream address is not allowed")
		}
	}
	return p.dialApproved(ctx, network, port, addresses)
}

type dialResult struct {
	conn net.Conn
	err  error
}

// dialGroup divides the remaining shared deadline among its approved addresses.
// Only one connection attempt in each address family is active at a time.
func (p *Upstream) dialGroup(ctx context.Context, network, port string, addresses []netip.Addr) (net.Conn, error) {
	var lastErr error
	for i, ip := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		deadline, _ := ctx.Deadline()
		attempt, cancel := context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(addresses)-i))
		conn, err := p.dial(attempt, network, net.JoinHostPort(ip.Unmap().String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// dialApproved races at most two family runners, preserving address order within
// each family. All candidates have passed policy validation before either starts.
func (p *Upstream) dialApproved(ctx context.Context, network, port string, addresses []netip.Addr) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var primary, secondary []netip.Addr
	firstIs4 := addresses[0].Unmap().Is4()
	for _, ip := range addresses {
		if ip.Unmap().Is4() == firstIs4 {
			primary = append(primary, ip)
		} else {
			secondary = append(secondary, ip)
		}
	}
	results := make(chan dialResult)
	start := func(group []netip.Addr) {
		go func() {
			conn, err := p.dialGroup(ctx, network, port, group)
			select {
			case results <- dialResult{conn, err}:
			case <-ctx.Done():
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()
	}
	start(primary)
	pending := 1
	fallbackStarted := len(secondary) == 0
	timer := time.NewTimer(p.fallbackDelay)
	defer timer.Stop()
	var timerC <-chan time.Time
	if !fallbackStarted {
		timerC = timer.C
	}
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timerC:
			timerC = nil
			fallbackStarted = true
			pending++
			start(secondary)
		case result := <-results:
			pending--
			if result.conn != nil {
				if err := ctx.Err(); err != nil {
					_ = result.conn.Close()
					return nil, err
				}
				return result.conn, nil
			}
			lastErr = result.err
			if !fallbackStarted {
				timer.Stop()
				timerC = nil
				fallbackStarted = true
				pending++
				start(secondary)
			}
			if pending == 0 {
				return nil, fmt.Errorf("upstream connection failed: %w", lastErr)
			}
		}
	}
}

type upstreamTransport struct {
	policy *Upstream
	base   http.RoundTripper
}

func (t *upstreamTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.policy.CheckURL(r.URL); err != nil {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, err
	}
	return t.base.RoundTrip(r)
}

// Transport creates a dedicated direct transport. Forward proxies cannot enforce
// this policy because they may resolve or route to a different destination.
func (p *Upstream) Transport(base *http.Transport) http.RoundTripper {
	guarded := base.Clone()
	guarded.Proxy = nil
	guarded.DialContext = p.DialContext
	guarded.DialTLSContext = nil
	guarded.DialTLS = nil //nolint:staticcheck // Clear legacy TLS hooks so they cannot bypass the guarded dialer.
	return &upstreamTransport{policy: p, base: guarded}
}
