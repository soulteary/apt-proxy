// Copyright 2026 Su Yang
// SPDX-License-Identifier: Apache-2.0

// Package metalink serves verified CentOS Metalink snapshots through the proxy.
package metalink

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/resource"
)

const (
	canonicalHost    = "metalink.apt-proxy.invalid"
	defaultSource    = "https://mirrors.centos.org/metalink"
	snapshotLifetime = 24 * time.Hour
)

// Config is opt-in. PublicURL is a fixed externally reachable proxy origin;
// client Host and forwarded headers never influence generated links.
type Config struct {
	PublicURL          string   `yaml:"public_url"`
	SourceURL          string   `yaml:"source_url"`
	AllowedMirrorHosts []string `yaml:"allowed_mirror_hosts"`
	RefreshSeconds     int      `yaml:"refresh_seconds"`
	TimeoutSeconds     int      `yaml:"timeout_seconds"`
	MaxXMLBytes        int64    `yaml:"max_xml_bytes"`
	MaxRepositories    int      `yaml:"max_repositories"`
	MaxRepomdBytes     int64    `yaml:"max_repomd_bytes"`
}

var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

func normalizeHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", fmt.Errorf("host zones are not allowed")
		}
		return ip.Unmap().String(), nil
	}
	if len(host) > 253 || !hostPattern.MatchString(host) {
		return "", fmt.Errorf("invalid exact mirror host")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("invalid exact mirror host")
		}
	}
	return host, nil
}

func safePath(path string) bool {
	if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\%\x00\r\n") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
		for _, c := range segment {
			if c < 32 || c == 127 {
				return false
			}
		}
	}
	return !strings.Contains(path, "//")
}

func parseURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.RawPath != "" {
		return nil, fmt.Errorf("URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if _, err := normalizeHost(u.Hostname()); err != nil {
		return nil, err
	}
	if u.Path != "" && !safePath(u.Path) {
		return nil, fmt.Errorf("unsafe URL path")
	}
	return u, nil
}

// New validates the entire configuration before constructing dedicated direct
// DNS-pinned transports. Upstream policy restrictions are never broadened.
func New(cfg *Config, upstream *access.UpstreamConfig, budgets ...*resource.Manager) (*Controller, error) {
	if cfg == nil {
		return nil, nil
	}
	cfgCopy := *cfg
	cfgCopy.AllowedMirrorHosts = append([]string(nil), cfg.AllowedMirrorHosts...)
	if cfgCopy.SourceURL == "" {
		cfgCopy.SourceURL = defaultSource
	}
	if cfgCopy.RefreshSeconds == 0 {
		cfgCopy.RefreshSeconds = 600
	}
	if cfgCopy.TimeoutSeconds == 0 {
		cfgCopy.TimeoutSeconds = 15
	}
	if cfgCopy.MaxXMLBytes == 0 {
		cfgCopy.MaxXMLBytes = 2 << 20
	}
	if cfgCopy.MaxRepomdBytes == 0 {
		cfgCopy.MaxRepomdBytes = 8 << 20
	}
	if cfgCopy.MaxRepositories == 0 {
		cfgCopy.MaxRepositories = 64
	}
	if cfgCopy.RefreshSeconds < 1 || cfgCopy.RefreshSeconds > 86400 || cfgCopy.TimeoutSeconds < 1 || cfgCopy.TimeoutSeconds > 120 || cfgCopy.MaxXMLBytes < 1024 || cfgCopy.MaxXMLBytes > 8<<20 || cfgCopy.MaxRepomdBytes < 1024 || cfgCopy.MaxRepomdBytes > 32<<20 || cfgCopy.MaxRepositories < 1 || cfgCopy.MaxRepositories > 64 {
		return nil, fmt.Errorf("metalink: configuration limits are out of range")
	}
	public, err := parseURL(cfgCopy.PublicURL)
	if err != nil || (public.Path != "" && public.Path != "/") {
		return nil, fmt.Errorf("metalink.public_url must be a fixed HTTP(S) origin")
	}
	public.Path = ""
	source, err := parseURL(cfgCopy.SourceURL)
	if err != nil {
		return nil, fmt.Errorf("metalink.source_url: %w", err)
	}
	if len(cfgCopy.AllowedMirrorHosts) == 0 || len(cfgCopy.AllowedMirrorHosts) > 128 {
		return nil, fmt.Errorf("metalink.allowed_mirror_hosts requires 1 to 128 exact hosts")
	}
	allowed := make(map[string]bool)
	for _, h := range cfgCopy.AllowedMirrorHosts {
		h, err = normalizeHost(h)
		if err != nil {
			return nil, fmt.Errorf("metalink.allowed_mirror_hosts: %w", err)
		}
		allowed[h] = true
	}
	outer, err := access.NewUpstream(upstream)
	if err != nil {
		return nil, err
	}
	if outer != nil {
		if err := outer.CheckURL(source); err != nil {
			return nil, fmt.Errorf("metalink source conflicts with upstream_access: %w", err)
		}
	}
	hosts := make([]string, 0, len(allowed)+1)
	for h := range allowed {
		hosts = append(hosts, h)
	}
	sourceHost, _ := normalizeHost(source.Hostname())
	hosts = append(hosts, sourceHost)
	sort.Strings(hosts)
	directCfg := &access.UpstreamConfig{Enabled: true, AllowedHosts: hosts}
	if outer != nil {
		directCfg.AllowedPorts = append([]int(nil), upstream.AllowedPorts...)
		directCfg.AllowCIDRs = append([]string(nil), upstream.AllowCIDRs...)
	}
	guard, err := access.NewUpstream(directCfg)
	if err != nil {
		return nil, err
	}
	if err := guard.CheckURL(source); err != nil {
		return nil, fmt.Errorf("metalink.source_url: %w", err)
	}
	base := &http.Transport{ResponseHeaderTimeout: time.Duration(cfgCopy.TimeoutSeconds) * time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second, DisableCompression: true}
	transport := guard.Transport(base)
	policyJSON, _ := json.Marshal(struct {
		Config   Config
		Upstream *access.UpstreamConfig
	}{cfgCopy, upstream})
	policy := sha256.Sum256(policyJSON)
	var budget *resource.Manager
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	return &Controller{cfg: cfgCopy, public: public, source: source, allowed: allowed, outer: outer, transport: transport, policy: hex.EncodeToString(policy[:]), now: time.Now, resources: budget, repos: map[string]*repository{}, snapshots: map[string]*snapshot{}}, nil
}
