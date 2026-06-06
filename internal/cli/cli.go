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

// Package cli provides the command-line interface for apt-proxy.
// It re-exports configuration types and parsing functions from internal/config
// for external use while maintaining the simplified cli.Daemon entry point.
package cli

import (
	"github.com/soulteary/apt-proxy/internal/config"
)

// Re-export types from internal/config for backward compatibility
type (
	// Config holds all application configuration
	Config = config.Config
	// TLSConfig holds TLS/HTTPS configuration
	TLSConfig = config.TLSConfig
	// MirrorConfig holds mirror-specific configuration
	MirrorConfig = config.MirrorConfig
	// CacheConfig holds cache-specific configuration
	CacheConfig = config.CacheConfig
	// StorageConfig selects and configures the cache storage backend.
	StorageConfig = config.StorageConfig
	// S3Config holds the S3-compatible storage credentials and tunables.
	S3Config = config.S3Config
)

// Re-export constants from internal/config for backward compatibility
const (
	EnvHost        = config.EnvHost
	EnvPort        = config.EnvPort
	EnvMode        = config.EnvMode
	EnvCacheDir    = config.EnvCacheDir
	EnvDebug       = config.EnvDebug
	EnvUbuntu      = config.EnvUbuntu
	EnvUbuntuPorts = config.EnvUbuntuPorts
	EnvDebian      = config.EnvDebian
	EnvCentOS      = config.EnvCentOS
	EnvAlpine      = config.EnvAlpine

	EnvCacheMaxSize         = config.EnvCacheMaxSize
	EnvCacheTTL             = config.EnvCacheTTL
	EnvCacheCleanupInterval = config.EnvCacheCleanupInterval

	EnvTLSEnabled  = config.EnvTLSEnabled
	EnvTLSCertFile = config.EnvTLSCertFile
	EnvTLSKeyFile  = config.EnvTLSKeyFile

	// Security (API)
	EnvAPIKey                = config.EnvAPIKey
	EnvEnableAPIAuth         = config.EnvEnableAPIAuth
	EnvAPIRateLimitPerMinute = config.EnvAPIRateLimitPerMinute
	EnvTrustedProxies        = config.EnvTrustedProxies

	// Upstream transport
	EnvUpstreamKeepAlive = config.EnvUpstreamKeepAlive

	// Configuration files
	EnvConfigFile          = config.EnvConfigFile
	EnvDistributionsConfig = config.EnvDistributionsConfig

	// Logging (no CLI equivalent; canonical + legacy aliases).
	EnvLogLevel        = config.EnvLogLevel
	EnvLogFormat       = config.EnvLogFormat
	EnvLogLevelLegacy  = config.EnvLogLevelLegacy
	EnvLogFormatLegacy = config.EnvLogFormatLegacy

	EnvStorageBackend = config.EnvStorageBackend
	EnvS3Endpoint     = config.EnvS3Endpoint
	EnvS3Region       = config.EnvS3Region
	EnvS3Bucket       = config.EnvS3Bucket
	EnvS3Prefix       = config.EnvS3Prefix
	EnvS3AccessKey    = config.EnvS3AccessKey
	EnvS3SecretKey    = config.EnvS3SecretKey
	EnvS3SessionToken = config.EnvS3SessionToken
	EnvS3UseSSL       = config.EnvS3UseSSL
	EnvS3UsePathStyle = config.EnvS3UsePathStyle
	EnvS3InlineMaxMB  = config.EnvS3InlineMaxMB
	EnvS3TempDir      = config.EnvS3TempDir

	StorageBackendDisk = config.StorageBackendDisk
	StorageBackendS3   = config.StorageBackendS3

	DefaultHost     = config.DefaultHost
	DefaultPort     = config.DefaultPort
	DefaultCacheDir = config.DefaultCacheDir

	DefaultCacheMaxSizeGB          = config.DefaultCacheMaxSizeGB
	DefaultCacheTTLHours           = config.DefaultCacheTTLHours
	DefaultCacheCleanupIntervalMin = config.DefaultCacheCleanupIntervalMin

	DefaultAPIRateLimitPerMinute = config.DefaultAPIRateLimitPerMinute
	DefaultConfigFileName        = config.DefaultConfigFileName

	DefaultStorageBackend = config.DefaultStorageBackend
	DefaultS3InlineMaxMB  = config.DefaultS3InlineMaxMB
	DefaultS3Prefix       = config.DefaultS3Prefix
)

// Backward compatibility wrapper for internal function
var allowedModes = config.GetAllowedModes()

// ParseFlags parses command-line flags and (when present) loads configuration
// from a YAML file. Priority: CLI > ENV > Config File > Default.
// Wrapper around config.ParseFlagsWithConfigFile, preserving the legacy entry name.
func ParseFlags() (*Config, error) {
	return config.ParseFlagsWithConfigFile()
}

// ValidateConfig validates the configuration.
// This is a wrapper around config.ValidateConfig for backward compatibility.
func ValidateConfig(cfg *Config) error {
	return config.ValidateConfig(cfg)
}

// modeToInt converts mode string to int for backward compatibility in tests
func modeToInt(mode string) int {
	return config.ModeToInt(mode)
}

// Build metadata, set by SetBuildInfo from the main package at startup.
var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// SetBuildInfo records the binary's build metadata so the daemon can log it.
// Called by main during startup with values injected via -ldflags.
func SetBuildInfo(version, commit, date string) {
	if version != "" {
		buildVersion = version
	}
	if commit != "" {
		buildCommit = commit
	}
	if date != "" {
		buildDate = date
	}
}

// BuildInfo returns the recorded build metadata.
func BuildInfo() (version, commit, date string) {
	return buildVersion, buildCommit, buildDate
}
