// Package config configuration merging logic between file/CLI/ENV sources.
package config

// MergeConfigsWithExplicit is like MergeConfigs but uses the explicit mask to
// decide whether a zero/empty value in `override` should overwrite `base`.
// Pass nil to fall back to MergeConfigs' permissive zero-value handling.
func MergeConfigsWithExplicit(base, override *Config, ex *cliExplicit) *Config {
	if ex == nil {
		return MergeConfigs(base, override)
	}
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := *base

	if ex.Debug {
		result.Debug = override.Debug
	}
	if ex.CacheDir && override.CacheDir != "" {
		result.CacheDir = override.CacheDir
	}
	if ex.Mode {
		result.Mode = override.Mode
	}
	if ex.Listen && override.Listen != "" {
		result.Listen = override.Listen
	}

	if ex.UbuntuMirror {
		result.Mirrors.Ubuntu = override.Mirrors.Ubuntu
	}
	if ex.UbuntuPortsMirror {
		result.Mirrors.UbuntuPorts = override.Mirrors.UbuntuPorts
	}
	if ex.DebianMirror {
		result.Mirrors.Debian = override.Mirrors.Debian
	}
	if ex.CentOSMirror {
		result.Mirrors.CentOS = override.Mirrors.CentOS
	}
	if ex.AlpineMirror {
		result.Mirrors.Alpine = override.Mirrors.Alpine
	}

	if ex.CacheMaxSize {
		result.Cache.MaxSize = override.Cache.MaxSize
	}
	if ex.CacheTTL {
		result.Cache.TTL = override.Cache.TTL
	}
	if ex.CacheCleanupInterval {
		result.Cache.CleanupInterval = override.Cache.CleanupInterval
	}

	if ex.TLSEnabled {
		result.TLS.Enabled = override.TLS.Enabled
	}
	if ex.TLSCertFile && override.TLS.CertFile != "" {
		result.TLS.CertFile = override.TLS.CertFile
	}
	if ex.TLSKeyFile && override.TLS.KeyFile != "" {
		result.TLS.KeyFile = override.TLS.KeyFile
	}

	if ex.APIKey && override.Security.APIKey != "" {
		result.Security.APIKey = override.Security.APIKey
	}
	if ex.EnableAPIAuth || ex.APIKey {
		// Setting an API key implies enabling auth (preserves legacy behaviour).
		result.Security.EnableAPIAuth = override.Security.EnableAPIAuth
	}
	if ex.APIRateLimitPerMinute {
		result.Security.APIRateLimitPerMinute = override.Security.APIRateLimitPerMinute
	}
	if ex.TrustedProxies {
		result.Security.TrustedProxies = append([]string(nil), override.Security.TrustedProxies...)
	}
	if ex.UpstreamKeepAlive {
		result.UpstreamKeepAlive = override.UpstreamKeepAlive
	}
	if ex.DistributionsConfig && override.DistributionsConfigPath != "" {
		result.DistributionsConfigPath = override.DistributionsConfigPath
	}

	return &result
}

// MergeConfigs merges two configurations, with values from 'override' taking precedence.
// Zero values in 'override' do not override values in 'base'.
//
// Deprecated: prefer MergeConfigsWithExplicit, which can distinguish "user
// wrote false/0" from "field defaulted". UpstreamKeepAlive in particular is
// a bool whose zero value (false) cannot be safely overridden here without
// the explicit-set mask.
func MergeConfigs(base, override *Config) *Config {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := *base

	// Override non-zero values
	if override.Debug {
		result.Debug = override.Debug
	}
	if override.CacheDir != "" {
		result.CacheDir = override.CacheDir
	}
	if override.Mode != 0 {
		result.Mode = override.Mode
	}
	if override.Listen != "" {
		result.Listen = override.Listen
	}

	// Merge MirrorConfig
	if override.Mirrors.Ubuntu != "" {
		result.Mirrors.Ubuntu = override.Mirrors.Ubuntu
	}
	if override.Mirrors.UbuntuPorts != "" {
		result.Mirrors.UbuntuPorts = override.Mirrors.UbuntuPorts
	}
	if override.Mirrors.Debian != "" {
		result.Mirrors.Debian = override.Mirrors.Debian
	}
	if override.Mirrors.CentOS != "" {
		result.Mirrors.CentOS = override.Mirrors.CentOS
	}
	if override.Mirrors.Alpine != "" {
		result.Mirrors.Alpine = override.Mirrors.Alpine
	}

	// Merge CacheConfig
	if override.Cache.MaxSize > 0 {
		result.Cache.MaxSize = override.Cache.MaxSize
	}
	if override.Cache.TTL > 0 {
		result.Cache.TTL = override.Cache.TTL
	}
	if override.Cache.CleanupInterval > 0 {
		result.Cache.CleanupInterval = override.Cache.CleanupInterval
	}

	// Merge TLSConfig
	if override.TLS.Enabled {
		result.TLS.Enabled = override.TLS.Enabled
	}
	if override.TLS.CertFile != "" {
		result.TLS.CertFile = override.TLS.CertFile
	}
	if override.TLS.KeyFile != "" {
		result.TLS.KeyFile = override.TLS.KeyFile
	}

	// Merge SecurityConfig
	if override.Security.APIKey != "" {
		result.Security.APIKey = override.Security.APIKey
	}
	if override.Security.EnableAPIAuth {
		result.Security.EnableAPIAuth = override.Security.EnableAPIAuth
	}
	if override.Security.APIRateLimitPerMinute > 0 {
		result.Security.APIRateLimitPerMinute = override.Security.APIRateLimitPerMinute
	}
	if len(override.Security.TrustedProxies) > 0 {
		result.Security.TrustedProxies = append([]string(nil), override.Security.TrustedProxies...)
	}
	if override.DistributionsConfigPath != "" {
		result.DistributionsConfigPath = override.DistributionsConfigPath
	}
	// UpstreamKeepAlive: override only when override is true (its non-zero
	// value). The legacy non-explicit merge cannot tell "user wrote false"
	// from "default false", so the safe behaviour is to never silently drop
	// a true value from base.
	if override.UpstreamKeepAlive {
		result.UpstreamKeepAlive = override.UpstreamKeepAlive
	}

	return &result
}
