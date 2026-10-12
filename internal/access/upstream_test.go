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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func mustPolicy(t *testing.T, cfg UpstreamConfig) *Upstream {
	t.Helper()
	p, err := NewUpstream(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestUpstreamURLPolicy(t *testing.T) {
	p := mustPolicy(t, UpstreamConfig{Enabled: true, AllowedHosts: []string{"Mirror.EXAMPLE."}, AllowedPorts: []int{443}})
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://mirror.example/file", true}, {"https://MIRROR.EXAMPLE.:443/file", true},
		{"http://mirror.example/file", false}, {"https://mirror.example:8443/file", false},
		{"https://mirror.example.attacker/file", false}, {"ftp://mirror.example/file", false},
		{"https:mirror.example/file", false}, {"https:///file", false},
	} {
		u, err := url.Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.CheckURL(u) == nil; got != tc.want {
			t.Fatalf("%s: allowed=%v", tc.url, got)
		}
	}
	if p.CheckURL(nil) == nil {
		t.Fatal("nil URL accepted")
	}
}
func TestUpstreamInvalidConfig(t *testing.T) {
	for _, cfg := range []UpstreamConfig{
		{Enabled: true, AllowedHosts: []string{"*.example.com"}}, {Enabled: true, AllowedHosts: []string{"https://mirror.example"}},
		{Enabled: true, AllowedHosts: []string{"bad..example"}}, {Enabled: true, AllowedPorts: []int{0}}, {Enabled: true, AllowedPorts: []int{65536}},
		{Enabled: true, AllowCIDRs: []string{""}}, {Enabled: true, AllowCIDRs: []string{"::ffff:127.0.0.0/104"}},
		{AllowedHosts: []string{"example.com"}},
	} {
		if _, err := NewUpstream(&cfg); err == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	for _, cfg := range []*UpstreamConfig{nil, {}} {
		p, err := NewUpstream(cfg)
		if err != nil || p != nil {
			t.Fatalf("default is not disabled: %v %v", p, err)
		}
	}
}
func TestUpstreamAddressPolicy(t *testing.T) {
	p := mustPolicy(t, UpstreamConfig{Enabled: true})
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "224.0.0.1", "255.255.255.255", "192.0.2.1", "198.18.0.1", "::1", "::", "fc00::1", "fe80::1", "ff02::1", "2001:db8::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::1"} {
		if p.addressAllowed(netip.MustParseAddr(ip)) {
			t.Fatalf("special address allowed: %s", ip)
		}
	}
	for _, ip := range []string{"93.184.216.34", "2606:4700::1111", "::ffff:93.184.216.34"} {
		if !p.addressAllowed(netip.MustParseAddr(ip)) {
			t.Fatalf("public address rejected: %s", ip)
		}
	}
	p = mustPolicy(t, UpstreamConfig{Enabled: true, AllowCIDRs: []string{"127.0.0.1/32", "10.42.0.0/16"}})
	if !p.addressAllowed(netip.MustParseAddr("127.0.0.1")) || !p.addressAllowed(netip.MustParseAddr("10.42.1.1")) || p.addressAllowed(netip.MustParseAddr("10.43.1.1")) {
		t.Fatal("exception scope incorrect")
	}
}
func TestUpstreamDNSPinningAndRebinding(t *testing.T) {
	p := mustPolicy(t, UpstreamConfig{Enabled: true})
	lookups, dials := 0, 0
	answers := []netip.Addr{netip.MustParseAddr("93.184.216.34")}
	p.lookup = func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookups++
		if network != "ip" || host != "mirror.example" {
			t.Fatalf("lookup=%s %s", network, host)
		}
		return answers, nil
	}
	p.dial = func(_ context.Context, network, address string) (net.Conn, error) {
		dials++
		if network != "tcp" || address != "93.184.216.34:443" {
			t.Fatalf("not pinned: %s %s", network, address)
		}
		return nil, errors.New("fixture dial")
	}
	_, _ = p.DialContext(context.Background(), "tcp", "mirror.example:443")
	if lookups != 1 || dials != 1 {
		t.Fatalf("lookups=%d dials=%d", lookups, dials)
	}
	for _, ips := range [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("169.254.169.254")}, nil} {
		answers = ips
		if _, err := p.DialContext(context.Background(), "tcp", "mirror.example:443"); err == nil {
			t.Fatal("unsafe DNS accepted")
		}
		if dials != 1 {
			t.Fatal("unsafe DNS dialed")
		}
	}
	if _, err := p.DialContext(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("literal loopback accepted")
	}
	if lookups != 4 || dials != 1 {
		t.Fatal("literal used resolver or dial")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	answers = []netip.Addr{netip.MustParseAddr("93.184.216.34")}
	_, err := p.DialContext(ctx, "tcp", "mirror.example:443")
	if !errors.Is(err, context.Canceled) || dials != 1 {
		t.Fatalf("canceled dial: %v", err)
	}
}
func TestUpstreamGuardedTransportDirectAndRedirect(t *testing.T) {
	trapHits, targetHits := 0, 0
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { trapHits++; w.WriteHeader(500) }))
	defer trap.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits++
		w.Header().Set("Location", "http://169.254.169.254/latest/meta-data/")
		w.WriteHeader(302)
	}))
	defer target.Close()
	t.Setenv("HTTP_PROXY", trap.URL)
	t.Setenv("HTTPS_PROXY", trap.URL)
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	number := 0
	for _, c := range port {
		number = number*10 + int(c-'0')
	}
	p := mustPolicy(t, UpstreamConfig{Enabled: true, AllowedPorts: []int{number, 80}, AllowCIDRs: []string{"127.0.0.1/32"}})
	transport := p.Transport(&http.Transport{Proxy: func(*http.Request) (*url.URL, error) { return url.Parse(trap.URL) }})
	req, err := http.NewRequest("GET", target.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 302 || trapHits != 0 || targetHits != 1 {
		t.Fatalf("status=%d trap=%d target=%d", resp.StatusCode, trapHits, targetHits)
	}
	// A redirect follow-up request through this same transport is rechecked.
	req, err = http.NewRequest("GET", resp.Header.Get("Location"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if follow, followErr := transport.RoundTrip(req); followErr == nil {
		_ = follow.Body.Close()
		t.Fatal("unsafe follow-up allowed")
	}
	denied := mustPolicy(t, UpstreamConfig{Enabled: true, AllowedPorts: []int{number}})
	if deniedResponse, deniedErr := denied.Transport(&http.Transport{}).RoundTrip(mustRequest(t, target.URL)); deniedErr == nil {
		_ = deniedResponse.Body.Close()
		t.Fatal("loopback connected without exception")
	}
	if targetHits != 1 {
		t.Fatal("denied transport reached target")
	}
}
func mustRequest(t *testing.T, uri string) *http.Request {
	t.Helper()
	r, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestUpstreamTLSPreservesAuthority(t *testing.T) {
	var host, sni string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		sni = r.TLS.ServerName
		w.WriteHeader(200)
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	names := server.Certificate().DNSNames
	if len(names) == 0 {
		t.Fatal("fixture certificate has no DNS name")
	}
	name := names[0]
	p := mustPolicy(t, UpstreamConfig{Enabled: true, AllowedHosts: []string{name}, AllowedPorts: []int{port}, AllowCIDRs: []string{"127.0.0.1/32"}})
	p.lookup = func(_ context.Context, _, hostname string) ([]netip.Addr, error) {
		if hostname != name {
			t.Fatalf("lookup=%s", hostname)
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	transport := p.Transport(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}})
	authority := net.JoinHostPort(name, u.Port())
	resp, err := transport.RoundTrip(mustRequest(t, "https://"+authority+"/file"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if host != authority || sni != name {
		t.Fatalf("Host=%s SNI=%s", host, sni)
	}
}

func TestUpstreamDNSDeadlineAndPolicySnapshot(t *testing.T) {
	cfg := UpstreamConfig{Enabled: true, AllowedHosts: []string{"mirror.example"}, AllowedPorts: []int{443}, AllowCIDRs: []string{"10.42.0.0/16"}}
	p := mustPolicy(t, cfg)
	cfg.AllowedHosts[0] = "evil.example"
	cfg.AllowedPorts[0] = 22
	cfg.AllowCIDRs[0] = "127.0.0.1/32"
	if err := p.authority("mirror.example", "443"); err != nil {
		t.Fatal("policy changed after configuration mutation", err)
	}
	if p.addressAllowed(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("exception list changed")
	}
	calls := 0
	p.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	}
	p.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("DNS deadline reached dial")
		return nil, nil
	}
	if _, err := p.DialContext(context.Background(), "tcp", "evil.example:443"); err == nil || calls != 0 {
		t.Fatal("denied host reached DNS")
	}
	if _, err := p.DialContext(context.Background(), "tcp", "mirror.example:22"); err == nil || calls != 0 {
		t.Fatal("denied port reached DNS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := p.DialContext(ctx, "tcp", "mirror.example:443"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DNS deadline error: %v", err)
	}
}

func racingPolicy(t *testing.T, ips ...string) *Upstream {
	t.Helper()
	p := mustPolicy(t, UpstreamConfig{Enabled: true})
	p.fallbackDelay = 20 * time.Millisecond
	addresses := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		addresses[i] = netip.MustParseAddr(ip)
	}
	p.lookup = func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil }
	return p
}

func TestUpstreamDualStackFallbackCancelsBlackhole(t *testing.T) {
	p := racingPolicy(t, "2606:4700::1111", "93.184.216.34")
	winner, peer := net.Pipe()
	defer peer.Close()
	canceled := make(chan struct{})
	p.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		if address == "[2606:4700::1111]:443" {
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}
		if address != "93.184.216.34:443" {
			return nil, fmt.Errorf("unexpected dial %s", address)
		}
		return winner, nil
	}
	start := time.Now()
	conn, err := p.DialContext(context.Background(), "tcp", "mirror.example:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if time.Since(start) > time.Second {
		t.Fatal("working family waited for blackholed address")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("losing dial not canceled")
	}
}

func TestUpstreamLateLosingConnectionIsClosed(t *testing.T) {
	p := racingPolicy(t, "2606:4700::1111", "93.184.216.34")
	loser, loserPeer := net.Pipe()
	defer loserPeer.Close()
	winner, winnerPeer := net.Pipe()
	defer winnerPeer.Close()
	p.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		if address == "[2606:4700::1111]:443" {
			<-ctx.Done()
			return loser, nil
		}
		return winner, nil
	}
	conn, err := p.DialContext(context.Background(), "tcp", "mirror.example:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := loserPeer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if _, err := loserPeer.Read(one[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("losing connection was not closed: %v", err)
	}
}

func TestUpstreamEarlyFailureStartsFallbackImmediately(t *testing.T) {
	p := racingPolicy(t, "2606:4700::1111", "93.184.216.34")
	p.fallbackDelay = 5 * time.Second
	winner, peer := net.Pipe()
	defer peer.Close()
	p.dial = func(_ context.Context, _, address string) (net.Conn, error) {
		if address == "[2606:4700::1111]:443" {
			return nil, errors.New("unreachable family")
		}
		return winner, nil
	}
	start := time.Now()
	conn, err := p.DialContext(context.Background(), "tcp", "mirror.example:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if time.Since(start) > time.Second {
		t.Fatal("early failure waited for fallback delay")
	}
}

func TestUpstreamSameFamilySharesDeadline(t *testing.T) {
	p := racingPolicy(t, "93.184.216.34", "93.184.216.35")
	p.dialTimeout = 120 * time.Millisecond
	winner, peer := net.Pipe()
	defer peer.Close()
	p.dial = func(ctx context.Context, _, address string) (net.Conn, error) {
		if address == "93.184.216.34:443" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return winner, nil
	}
	conn, err := p.DialContext(context.Background(), "tcp", "mirror.example:443")
	if err != nil {
		t.Fatalf("first address consumed entire budget: %v", err)
	}
	defer conn.Close()
}

func TestUpstreamSharedDeadlineCancelsBothFamilies(t *testing.T) {
	p := racingPolicy(t, "2606:4700::1111", "93.184.216.34")
	p.dialTimeout = 100 * time.Millisecond
	finished := make(chan struct{}, 2)
	p.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		finished <- struct{}{}
		return nil, ctx.Err()
	}
	conn, err := p.DialContext(context.Background(), "tcp", "mirror.example:443")
	if conn != nil {
		_ = conn.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shared deadline: %v", err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("family runner did not stop")
		}
	}
}
