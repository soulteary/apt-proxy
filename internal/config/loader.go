// Package config provides configuration loading orchestration. The actual
// CLI/ENV resolution lives in loader_flags.go, YAML handling in loader_yaml.go,
// merging in loader_merge.go, validation in loader_validate.go, and config
// file path search in loader_search.go.
package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/cli-kit/configutil"
	httpcache "github.com/soulteary/httpcache-kit"
)

// ParseFlags parses command-line flags and returns a Config struct with all
// application settings. It validates the mode parameter and sets up global state.
// Configuration priority: CLI flag > environment variable > default value.
// Returns an error if flag parsing fails or if an invalid mode is specified.
func ParseFlags() (*Config, error) {
	flags := flag.NewFlagSet("apt-proxy", flag.ContinueOnError)
	defineFlags(flags)

	if err := flags.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	// Validate and resolve mode using enum validation
	modeName, err := configutil.ResolveEnum(flags, "mode", EnvMode, distro.DistroAll, allowedModes, false)
	if err != nil {
		return nil, fmt.Errorf("invalid mode: %w", err)
	}

	// Build CLI configuration with defaults
	config, ex := buildCLIConfig(flags, DefaultHost, DefaultPort, DefaultCacheDir, DefaultCacheMaxSizeGB, DefaultCacheTTLHours, DefaultCacheCleanupIntervalMin)

	// Set mode (buildCLIConfig may have set it, but we ensure it's set here with validated value)
	config.Mode = ModeToInt(modeName)

	// Apply defaults for cache only when the user did not explicitly request
	// a zero value (e.g. `--cache-max-size=0` should genuinely disable the limit).
	if config.Cache.MaxSize == 0 && !ex.CacheMaxSize {
		config.Cache.MaxSize = httpcache.DefaultMaxCacheSize
	}
	if config.Cache.TTL == 0 && !ex.CacheTTL {
		config.Cache.TTL = httpcache.DefaultCacheTTL
	}
	if config.Cache.CleanupInterval == 0 && !ex.CacheCleanupInterval {
		config.Cache.CleanupInterval = httpcache.DefaultCleanupInterval
	}

	// Set listen address if not already set
	if config.Listen == "" {
		host := configutil.ResolveString(flags, "host", EnvHost, DefaultHost, true)
		port := configutil.ResolveString(flags, "port", EnvPort, DefaultPort, true)
		config.Listen = mirrors.BuildListenAddress(host, port)
	}

	// Update global state
	if err := UpdateGlobalState(config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return config, nil
}

// ParseFlagsWithConfigFile parses command-line flags and optionally loads
// configuration from a YAML file. Priority: CLI > ENV > Config File > Default.
func ParseFlagsWithConfigFile() (*Config, error) {
	flags := flag.NewFlagSet("apt-proxy", flag.ContinueOnError)
	defineFlags(flags)

	if err := flags.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	// Try to load configuration from file
	var fileConfig *Config
	configPath := configutil.ResolveString(flags, "config", EnvConfigFile, "", true)
	if configPath == "" {
		configPath = FindConfigFile()
	}
	if configPath != "" {
		var err error
		fileConfig, err = LoadConfigFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("loading config file %s: %w", configPath, err)
		}
	}

	// Build CLI/ENV configuration
	cliConfig, ex := buildCLIConfig(flags, "", "", "", 0, 0, 0)

	// Merge configurations: file config as base, CLI/ENV as override (honoring
	// the explicit-set mask so users can override file/defaults with false/0).
	config := MergeConfigsWithExplicit(fileConfig, cliConfig, ex)

	// Apply defaults for any remaining unset values, but respect explicit
	// CLI/ENV zeroes (e.g. --cache-max-size=0 must really disable the limit).
	config = applyDefaultsWithExplicit(config, ex)

	// Update global state
	if err := UpdateGlobalState(config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return config, nil
}

// applyDefaults fills in defaults for unset fields using the conservative
// (no-explicit-mask) policy. Prefer applyDefaultsWithExplicit when the
// caller has tracked CLI/ENV explicit-ness.
func applyDefaults(config *Config) *Config {
	return applyDefaultsWithExplicit(config, nil)
}

// applyDefaultsWithExplicit fills in defaults for unset fields, but treats
// CacheMaxSize/CacheTTL/CacheCleanupInterval as "user wanted zero" when the
// explicit mask says they were set on CLI/ENV. Without this, an explicit
// `--cache-max-size=0` would be silently rewritten to the kit default.
func applyDefaultsWithExplicit(config *Config, ex *cliExplicit) *Config {
	if config == nil {
		config = &Config{}
	}

	if config.CacheDir == "" {
		config.CacheDir = DefaultCacheDir
	}

	if config.Listen == "" {
		config.Listen = mirrors.BuildListenAddress(DefaultHost, DefaultPort)
	}

	if config.Mode == 0 {
		config.Mode = distro.TypeAllDistros
	}

	// Apply cache defaults only when the user did not explicitly request the
	// zero value via CLI/ENV.
	if config.Cache.MaxSize == 0 && (ex == nil || !ex.CacheMaxSize) {
		config.Cache.MaxSize = httpcache.DefaultMaxCacheSize
	}
	if config.Cache.TTL == 0 && (ex == nil || !ex.CacheTTL) {
		config.Cache.TTL = httpcache.DefaultCacheTTL
	}
	if config.Cache.CleanupInterval == 0 && (ex == nil || !ex.CacheCleanupInterval) {
		config.Cache.CleanupInterval = httpcache.DefaultCleanupInterval
	}

	return config
}
