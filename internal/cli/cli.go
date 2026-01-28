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

	DefaultHost     = config.DefaultHost
	DefaultPort     = config.DefaultPort
	DefaultCacheDir = config.DefaultCacheDir

	DefaultCacheMaxSizeGB          = config.DefaultCacheMaxSizeGB
	DefaultCacheTTLHours           = config.DefaultCacheTTLHours
	DefaultCacheCleanupIntervalMin = config.DefaultCacheCleanupIntervalMin
)

// Backward compatibility wrapper for internal function
var allowedModes = config.GetAllowedModes()

// ParseFlags parses command-line flags and returns a Config.
// This is a wrapper around config.ParseFlags for backward compatibility.
func ParseFlags() (*Config, error) {
	return config.ParseFlags()
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
