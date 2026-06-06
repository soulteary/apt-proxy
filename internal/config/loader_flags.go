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

// Package config CLI flag definitions and CLI/ENV resolution helpers.
package config

import (
	"flag"
	"os"
	"strings"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/cli-kit/configutil"
)

var (
	// allowedModes defines the valid mode values for proxy operation
	allowedModes = []string{
		distro.DistroAll,
		distro.DistroUbuntu,
		distro.DistroUbuntuPorts,
		distro.DistroDebian,
		distro.DistroCentOS,
		distro.DistroAlpine,
	}
)

// GetAllowedModes returns the list of allowed mode values
func GetAllowedModes() []string {
	return allowedModes
}

// ModeToInt converts a validated mode string to its corresponding integer constant.
// This function should only be called after mode validation via configutil.ResolveEnum.
func ModeToInt(mode string) int {
	switch mode {
	case distro.DistroUbuntu:
		return distro.TypeUbuntu
	case distro.DistroUbuntuPorts:
		return distro.TypeUbuntuPorts
	case distro.DistroDebian:
		return distro.TypeDebian
	case distro.DistroCentOS:
		return distro.TypeCentOS
	case distro.DistroAlpine:
		return distro.TypeAlpine
	default:
		return distro.TypeAllDistros
	}
}

// defineFlags defines all command-line flags for the application.
// This function is shared between ParseFlags and ParseFlagsWithConfigFile.
func defineFlags(flags *flag.FlagSet) {
	// Define flags (for CLI compatibility and help text)
	flags.String("host", DefaultHost, "the host to bind to")
	flags.String("port", DefaultPort, "the port to bind to")
	flags.String("mode", distro.DistroAll,
		"select the mode of system to cache: all / ubuntu / ubuntu-ports / debian / centos / alpine")
	flags.Bool("debug", false, "whether to output debugging logging")
	flags.String("cachedir", DefaultCacheDir, "the dir to store cache data in")
	flags.String("ubuntu", "", "the ubuntu mirror for fetching packages")
	flags.String("ubuntu-ports", "", "the ubuntu ports mirror for fetching packages")
	flags.String("debian", "", "the debian mirror for fetching packages")
	flags.String("centos", "", "the centos mirror for fetching packages")
	flags.String("alpine", "", "the alpine mirror for fetching packages")
	flags.String("distributions-config", "", "path to distributions YAML (distributions.yaml)")

	// Cache configuration flags
	flags.Int64("cache-max-size", DefaultCacheMaxSizeGB,
		"maximum cache size in GB (0 to disable size limit)")
	flags.Int("cache-ttl", DefaultCacheTTLHours,
		"cache TTL in hours (0 to disable TTL-based eviction)")
	flags.Int("cache-cleanup-interval", DefaultCacheCleanupIntervalMin,
		"cache cleanup interval in minutes (0 to disable automatic cleanup)")

	// TLS configuration flags
	flags.Bool("tls", false, "enable TLS/HTTPS")
	flags.String("tls-cert", "", "path to TLS certificate file")
	flags.String("tls-key", "", "path to TLS private key file")

	// Security: API rate limit (0 = disabled)
	flags.Int("api-rate-limit", DefaultAPIRateLimitPerMinute, "API requests per IP per minute (0=disabled)")
	// Security: explicit toggle for API authentication.
	// If APIKey is set this is auto-enabled; flag/ENV lets users force it off
	// or enable a placeholder for forward-compatible deployments.
	flags.Bool("enable-api-auth", false, "explicitly enable API authentication middleware")
	// Security: API key for protected endpoints (also auto-enables auth)
	flags.String("api-key", "", "API key for protected endpoints")
	// Security: trusted proxies for honoring X-Forwarded-For (comma-separated CIDRs)
	flags.String("trusted-proxies", "", "comma-separated CIDRs whose X-Forwarded-For is trusted by the API rate-limiter")
	// Configuration file (only honored by ParseFlagsWithConfigFile)
	flags.String("config", "", "path to YAML configuration file")

	// Upstream: keep-alive to mirrors (default true)
	flags.Bool("upstream-keep-alive", true, "enable HTTP keep-alive to upstream mirrors")
}

// cliExplicit tracks which fields were explicitly set on the CLI / via ENV.
// This lets MergeConfigsWithExplicit distinguish "user wrote false/0" from
// "field defaulted to false/0", which the legacy MergeConfigs cannot.
type cliExplicit struct {
	Debug                 bool
	CacheDir              bool
	Mode                  bool
	Listen                bool
	UbuntuMirror          bool
	UbuntuPortsMirror     bool
	DebianMirror          bool
	CentOSMirror          bool
	AlpineMirror          bool
	CacheMaxSize          bool
	CacheTTL              bool
	CacheCleanupInterval  bool
	TLSEnabled            bool
	TLSCertFile           bool
	TLSKeyFile            bool
	APIKey                bool
	EnableAPIAuth         bool
	APIRateLimitPerMinute bool
	TrustedProxies        bool
	UpstreamKeepAlive     bool
	DistributionsConfig   bool
}

// flagOrEnvSet reports whether a CLI flag or its environment variable was
// explicitly provided. We can't simply consult flag.Visit because the
// configutil.Resolve* helpers fall back to ENV without flagging the flag
// as visited; checking both sources mirrors what the resolver does.
func flagOrEnvSet(flags *flag.FlagSet, name, env string) bool {
	if flags != nil {
		var seen bool
		flags.Visit(func(f *flag.Flag) {
			if f.Name == name {
				seen = true
			}
		})
		if seen {
			return true
		}
	}
	if env != "" {
		if _, ok := os.LookupEnv(env); ok {
			return true
		}
	}
	return false
}

// buildCLIConfig builds a Config from CLI flags and environment variables.
// Default values are used when flags/env vars are not set.
// The returned cliExplicit mask records which fields were explicitly set
// (CLI flag or ENV) so MergeConfigsWithExplicit can honor false/0 overrides.
func buildCLIConfig(flags *flag.FlagSet, defaultHost, defaultPort, defaultCacheDir string, defaultCacheMaxSizeGB int64, defaultCacheTTLHours, defaultCacheCleanupIntervalMin int) (*Config, *cliExplicit) {
	ex := &cliExplicit{
		Debug:                 flagOrEnvSet(flags, "debug", EnvDebug),
		CacheDir:              flagOrEnvSet(flags, "cachedir", EnvCacheDir),
		Mode:                  flagOrEnvSet(flags, "mode", EnvMode),
		UbuntuMirror:          flagOrEnvSet(flags, "ubuntu", EnvUbuntu),
		UbuntuPortsMirror:     flagOrEnvSet(flags, "ubuntu-ports", EnvUbuntuPorts),
		DebianMirror:          flagOrEnvSet(flags, "debian", EnvDebian),
		CentOSMirror:          flagOrEnvSet(flags, "centos", EnvCentOS),
		AlpineMirror:          flagOrEnvSet(flags, "alpine", EnvAlpine),
		CacheMaxSize:          flagOrEnvSet(flags, "cache-max-size", EnvCacheMaxSize),
		CacheTTL:              flagOrEnvSet(flags, "cache-ttl", EnvCacheTTL),
		CacheCleanupInterval:  flagOrEnvSet(flags, "cache-cleanup-interval", EnvCacheCleanupInterval),
		TLSEnabled:            flagOrEnvSet(flags, "tls", EnvTLSEnabled),
		TLSCertFile:           flagOrEnvSet(flags, "tls-cert", EnvTLSCertFile),
		TLSKeyFile:            flagOrEnvSet(flags, "tls-key", EnvTLSKeyFile),
		APIKey:                flagOrEnvSet(flags, "api-key", EnvAPIKey),
		EnableAPIAuth:         flagOrEnvSet(flags, "enable-api-auth", EnvEnableAPIAuth),
		APIRateLimitPerMinute: flagOrEnvSet(flags, "api-rate-limit", EnvAPIRateLimitPerMinute),
		TrustedProxies:        flagOrEnvSet(flags, "trusted-proxies", EnvTrustedProxies),
		UpstreamKeepAlive:     flagOrEnvSet(flags, "upstream-keep-alive", EnvUpstreamKeepAlive),
		DistributionsConfig:   flagOrEnvSet(flags, "distributions-config", EnvDistributionsConfig),
	}
	hostSet := flagOrEnvSet(flags, "host", EnvHost)
	portSet := flagOrEnvSet(flags, "port", EnvPort)
	ex.Listen = hostSet || portSet

	host := configutil.ResolveString(flags, "host", EnvHost, defaultHost, true)
	port := configutil.ResolveString(flags, "port", EnvPort, defaultPort, true)
	debug := configutil.ResolveBool(flags, "debug", EnvDebug, false)
	cacheDir := configutil.ResolveString(flags, "cachedir", EnvCacheDir, defaultCacheDir, true)

	// Resolve mode
	modeName, _ := configutil.ResolveEnum(flags, "mode", EnvMode, "", allowedModes, false)

	// Resolve distributions config path
	distributionsConfig := configutil.ResolveString(flags, "distributions-config", EnvDistributionsConfig, "", true)

	// Resolve mirror configurations
	ubuntu := configutil.ResolveString(flags, "ubuntu", EnvUbuntu, "", true)
	ubuntuPorts := configutil.ResolveString(flags, "ubuntu-ports", EnvUbuntuPorts, "", true)
	debian := configutil.ResolveString(flags, "debian", EnvDebian, "", true)
	centos := configutil.ResolveString(flags, "centos", EnvCentOS, "", true)
	alpine := configutil.ResolveString(flags, "alpine", EnvAlpine, "", true)

	// Resolve cache configurations
	cacheMaxSizeGB := configutil.ResolveInt64(flags, "cache-max-size", EnvCacheMaxSize, defaultCacheMaxSizeGB, true)
	cacheTTLHours := configutil.ResolveInt(flags, "cache-ttl", EnvCacheTTL, defaultCacheTTLHours, true)
	cacheCleanupIntervalMin := configutil.ResolveInt(flags, "cache-cleanup-interval", EnvCacheCleanupInterval, defaultCacheCleanupIntervalMin, true)

	// Resolve TLS configurations
	tlsEnabled := configutil.ResolveBool(flags, "tls", EnvTLSEnabled, false)
	tlsCertFile := configutil.ResolveString(flags, "tls-cert", EnvTLSCertFile, "", true)
	tlsKeyFile := configutil.ResolveString(flags, "tls-key", EnvTLSKeyFile, "", true)

	// Resolve security configurations
	apiKey := configutil.ResolveString(flags, "api-key", EnvAPIKey, "", true)
	// Use the dedicated bool flag/ENV for explicit toggle; previously this
	// erroneously reused the "api-key" String flag name which never produced
	// a usable bool value. Setting APIKey only auto-enables auth when the
	// user did NOT explicitly pass --enable-api-auth (CLI/ENV); an explicit
	// `--enable-api-auth=false` must win even with --api-key set.
	enableAPIAuth := configutil.ResolveBool(flags, "enable-api-auth", EnvEnableAPIAuth, false)
	enableAPIAuthExplicit := flagOrEnvSet(flags, "enable-api-auth", EnvEnableAPIAuth)
	if apiKey != "" && !enableAPIAuthExplicit {
		enableAPIAuth = true
	}
	apiRateLimitPerMinute := configutil.ResolveInt(flags, "api-rate-limit", EnvAPIRateLimitPerMinute, DefaultAPIRateLimitPerMinute, true)
	upstreamKeepAlive := configutil.ResolveBool(flags, "upstream-keep-alive", EnvUpstreamKeepAlive, true)
	trustedProxiesRaw := configutil.ResolveString(flags, "trusted-proxies", EnvTrustedProxies, "", true)
	var trustedProxies []string
	if trustedProxiesRaw != "" {
		for _, p := range strings.Split(trustedProxiesRaw, ",") {
			if v := strings.TrimSpace(p); v != "" {
				trustedProxies = append(trustedProxies, v)
			}
		}
	}

	config := &Config{
		Debug:             debug,
		CacheDir:          cacheDir,
		UpstreamKeepAlive: upstreamKeepAlive,
		Mirrors: MirrorConfig{
			Ubuntu:      ubuntu,
			UbuntuPorts: ubuntuPorts,
			Debian:      debian,
			CentOS:      centos,
			Alpine:      alpine,
		},
		Cache: CacheConfig{
			MaxSize:         cacheMaxSizeGB * 1024 * 1024 * 1024,
			TTL:             time.Duration(cacheTTLHours) * time.Hour,
			CleanupInterval: time.Duration(cacheCleanupIntervalMin) * time.Minute,
		},
		TLS: TLSConfig{
			Enabled:  tlsEnabled,
			CertFile: tlsCertFile,
			KeyFile:  tlsKeyFile,
		},
		Security: SecurityConfig{
			APIKey:                apiKey,
			EnableAPIAuth:         enableAPIAuth,
			APIRateLimitPerMinute: apiRateLimitPerMinute,
			TrustedProxies:        trustedProxies,
		},
		DistributionsConfigPath: distributionsConfig,
	}

	// Set mode if specified
	if modeName != "" {
		config.Mode = ModeToInt(modeName)
	}

	// Set listen address if host or port specified
	if host != "" || port != "" {
		if host == "" {
			host = defaultHost
		}
		if port == "" {
			port = defaultPort
		}
		config.Listen = mirrors.BuildListenAddress(host, port)
	}

	return config, ex
}
