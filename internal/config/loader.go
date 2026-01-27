package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v2"

	"github.com/soulteary/apt-proxy/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/pkg/httpcache"
	"github.com/soulteary/apt-proxy/state"
	"github.com/soulteary/cli-kit/configutil"
)

var (
	// allowedModes defines the valid mode values for proxy operation
	allowedModes = []string{
		distro.LINUX_ALL_DISTROS,
		distro.LINUX_DISTROS_UBUNTU,
		distro.LINUX_DISTROS_UBUNTU_PORTS,
		distro.LINUX_DISTROS_DEBIAN,
		distro.LINUX_DISTROS_CENTOS,
		distro.LINUX_DISTROS_ALPINE,
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
	case distro.LINUX_DISTROS_UBUNTU:
		return distro.TYPE_LINUX_DISTROS_UBUNTU
	case distro.LINUX_DISTROS_UBUNTU_PORTS:
		return distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS
	case distro.LINUX_DISTROS_DEBIAN:
		return distro.TYPE_LINUX_DISTROS_DEBIAN
	case distro.LINUX_DISTROS_CENTOS:
		return distro.TYPE_LINUX_DISTROS_CENTOS
	case distro.LINUX_DISTROS_ALPINE:
		return distro.TYPE_LINUX_DISTROS_ALPINE
	default:
		return distro.TYPE_LINUX_ALL_DISTROS
	}
}

// ParseFlags parses command-line flags and returns a Config struct with all
// application settings. It validates the mode parameter and sets up global state.
// Configuration priority: CLI flag > environment variable > default value.
// Returns an error if flag parsing fails or if an invalid mode is specified.
func ParseFlags() (*Config, error) {
	flags := flag.NewFlagSet("apt-proxy", flag.ContinueOnError)

	// Define flags (for CLI compatibility and help text)
	flags.String("host", DefaultHost, "the host to bind to")
	flags.String("port", DefaultPort, "the port to bind to")
	flags.String("mode", distro.LINUX_ALL_DISTROS,
		"select the mode of system to cache: all / ubuntu / ubuntu-ports / debian / centos / alpine")
	flags.Bool("debug", false, "whether to output debugging logging")
	flags.String("cachedir", DefaultCacheDir, "the dir to store cache data in")
	flags.String("ubuntu", "", "the ubuntu mirror for fetching packages")
	flags.String("ubuntu-ports", "", "the ubuntu ports mirror for fetching packages")
	flags.String("debian", "", "the debian mirror for fetching packages")
	flags.String("centos", "", "the centos mirror for fetching packages")
	flags.String("alpine", "", "the alpine mirror for fetching packages")

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

	if err := flags.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	// Resolve configuration with priority: CLI > ENV > default
	host := configutil.ResolveString(flags, "host", EnvHost, DefaultHost, true)
	port := configutil.ResolveString(flags, "port", EnvPort, DefaultPort, true)
	debug := configutil.ResolveBool(flags, "debug", EnvDebug, false)
	cacheDir := configutil.ResolveString(flags, "cachedir", EnvCacheDir, DefaultCacheDir, true)

	// Validate and resolve mode using enum validation
	modeName, err := configutil.ResolveEnum(flags, "mode", EnvMode, distro.LINUX_ALL_DISTROS, allowedModes, false)
	if err != nil {
		return nil, fmt.Errorf("invalid mode: %w", err)
	}

	// Resolve mirror configurations
	ubuntu := configutil.ResolveString(flags, "ubuntu", EnvUbuntu, "", true)
	ubuntuPorts := configutil.ResolveString(flags, "ubuntu-ports", EnvUbuntuPorts, "", true)
	debian := configutil.ResolveString(flags, "debian", EnvDebian, "", true)
	centos := configutil.ResolveString(flags, "centos", EnvCentOS, "", true)
	alpine := configutil.ResolveString(flags, "alpine", EnvAlpine, "", true)

	// Resolve cache configurations
	cacheMaxSizeGB := configutil.ResolveInt64(flags, "cache-max-size", EnvCacheMaxSize, DefaultCacheMaxSizeGB, true)
	cacheTTLHours := configutil.ResolveInt(flags, "cache-ttl", EnvCacheTTL, DefaultCacheTTLHours, true)
	cacheCleanupIntervalMin := configutil.ResolveInt(flags, "cache-cleanup-interval", EnvCacheCleanupInterval, DefaultCacheCleanupIntervalMin, true)

	// Resolve TLS configurations
	tlsEnabled := configutil.ResolveBool(flags, "tls", EnvTLSEnabled, false)
	tlsCertFile := configutil.ResolveString(flags, "tls-cert", EnvTLSCertFile, "", true)
	tlsKeyFile := configutil.ResolveString(flags, "tls-key", EnvTLSKeyFile, "", true)

	// Build configuration
	config := Config{
		Debug:    debug,
		CacheDir: cacheDir,
		Mode:     ModeToInt(modeName),
		Mirrors: MirrorConfig{
			Ubuntu:      ubuntu,
			UbuntuPorts: ubuntuPorts,
			Debian:      debian,
			CentOS:      centos,
			Alpine:      alpine,
		},
		Cache: CacheConfig{
			MaxSize:         cacheMaxSizeGB * 1024 * 1024 * 1024, // Convert GB to bytes
			TTL:             time.Duration(cacheTTLHours) * time.Hour,
			CleanupInterval: time.Duration(cacheCleanupIntervalMin) * time.Minute,
		},
		TLS: TLSConfig{
			Enabled:  tlsEnabled,
			CertFile: tlsCertFile,
			KeyFile:  tlsKeyFile,
		},
	}

	// Use defaults from httpcache if values are 0 (meaning use default)
	if config.Cache.MaxSize == 0 {
		config.Cache.MaxSize = httpcache.DefaultMaxCacheSize
	}
	if config.Cache.TTL == 0 {
		config.Cache.TTL = httpcache.DefaultCacheTTL
	}
	if config.Cache.CleanupInterval == 0 {
		config.Cache.CleanupInterval = httpcache.DefaultCleanupInterval
	}

	// Set listen address using templates
	listenAddr, err := mirrors.BuildListenAddress(host, port)
	if err != nil {
		// Fallback to fmt.Sprintf if template fails
		config.Listen = fmt.Sprintf("%s:%s", host, port)
	} else {
		config.Listen = listenAddr
	}

	// Update global state
	if err := UpdateGlobalState(&config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return &config, nil
}

// UpdateGlobalState updates the global state with the current configuration,
// including proxy mode and mirror URLs for all supported distributions.
// This enables components throughout the application to access configuration.
func UpdateGlobalState(config *Config) error {
	state.SetProxyMode(config.Mode)

	state.SetUbuntuMirror(config.Mirrors.Ubuntu)
	state.SetUbuntuPortsMirror(config.Mirrors.UbuntuPorts)
	state.SetDebianMirror(config.Mirrors.Debian)
	state.SetCentOSMirror(config.Mirrors.CentOS)
	state.SetAlpineMirror(config.Mirrors.Alpine)

	return nil
}

// ValidateConfig performs validation on the configuration to ensure all required
// fields are set and valid. Returns an error if validation fails.
func ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	if config.CacheDir == "" {
		return fmt.Errorf("cache directory must be specified")
	}

	if config.Listen == "" {
		return fmt.Errorf("listen address must be specified")
	}

	// Validate TLS configuration
	if config.TLS.Enabled {
		if config.TLS.CertFile == "" {
			return fmt.Errorf("TLS certificate file must be specified when TLS is enabled")
		}
		if config.TLS.KeyFile == "" {
			return fmt.Errorf("TLS key file must be specified when TLS is enabled")
		}
		// Check if certificate file exists
		if _, err := os.Stat(config.TLS.CertFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS certificate file not found: %s", config.TLS.CertFile)
		}
		// Check if key file exists
		if _, err := os.Stat(config.TLS.KeyFile); os.IsNotExist(err) {
			return fmt.Errorf("TLS key file not found: %s", config.TLS.KeyFile)
		}
	}

	return nil
}

// YAMLConfig represents the YAML configuration file structure.
// It uses a more user-friendly structure that maps to the internal Config.
type YAMLConfig struct {
	Server struct {
		Host  string `yaml:"host"`
		Port  string `yaml:"port"`
		Debug bool   `yaml:"debug"`
	} `yaml:"server"`

	Cache struct {
		Dir                string `yaml:"dir"`
		MaxSizeGB          int64  `yaml:"max_size_gb"`
		TTLHours           int    `yaml:"ttl_hours"`
		CleanupIntervalMin int    `yaml:"cleanup_interval_min"`
	} `yaml:"cache"`

	Mirrors struct {
		Ubuntu      string `yaml:"ubuntu"`
		UbuntuPorts string `yaml:"ubuntu_ports"`
		Debian      string `yaml:"debian"`
		CentOS      string `yaml:"centos"`
		Alpine      string `yaml:"alpine"`
	} `yaml:"mirrors"`

	TLS struct {
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`

	Security struct {
		APIKey        string `yaml:"api_key"`
		EnableAPIAuth bool   `yaml:"enable_api_auth"`
	} `yaml:"security"`

	Mode string `yaml:"mode"`
}

// LoadConfigFile loads configuration from a YAML file.
// It returns nil if the file does not exist.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist, not an error
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in the YAML content
	expandedData := os.ExpandEnv(string(data))

	var yamlCfg YAMLConfig
	if err := yaml.Unmarshal([]byte(expandedData), &yamlCfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return yamlConfigToConfig(&yamlCfg), nil
}

// yamlConfigToConfig converts a YAMLConfig to the internal Config structure.
func yamlConfigToConfig(yamlCfg *YAMLConfig) *Config {
	cfg := &Config{
		Debug:    yamlCfg.Server.Debug,
		CacheDir: yamlCfg.Cache.Dir,
		Mirrors: MirrorConfig{
			Ubuntu:      yamlCfg.Mirrors.Ubuntu,
			UbuntuPorts: yamlCfg.Mirrors.UbuntuPorts,
			Debian:      yamlCfg.Mirrors.Debian,
			CentOS:      yamlCfg.Mirrors.CentOS,
			Alpine:      yamlCfg.Mirrors.Alpine,
		},
		Cache: CacheConfig{
			MaxSizeGB:          yamlCfg.Cache.MaxSizeGB,
			TTLHours:           yamlCfg.Cache.TTLHours,
			CleanupIntervalMin: yamlCfg.Cache.CleanupIntervalMin,
		},
		TLS: TLSConfig{
			Enabled:  yamlCfg.TLS.Enabled,
			CertFile: yamlCfg.TLS.CertFile,
			KeyFile:  yamlCfg.TLS.KeyFile,
		},
		Security: SecurityConfig{
			APIKey:        yamlCfg.Security.APIKey,
			EnableAPIAuth: yamlCfg.Security.EnableAPIAuth,
		},
	}

	// Convert mode string to int
	if yamlCfg.Mode != "" {
		cfg.Mode = ModeToInt(yamlCfg.Mode)
	}

	// Build listen address from host and port
	host := yamlCfg.Server.Host
	port := yamlCfg.Server.Port
	if host != "" || port != "" {
		if host == "" {
			host = DefaultHost
		}
		if port == "" {
			port = DefaultPort
		}
		cfg.Listen = fmt.Sprintf("%s:%s", host, port)
	}

	// Convert GB to bytes for MaxSize
	if cfg.Cache.MaxSizeGB > 0 {
		cfg.Cache.MaxSize = cfg.Cache.MaxSizeGB * 1024 * 1024 * 1024
	}

	// Convert hours to duration for TTL
	if cfg.Cache.TTLHours > 0 {
		cfg.Cache.TTL = time.Duration(cfg.Cache.TTLHours) * time.Hour
	}

	// Convert minutes to duration for CleanupInterval
	if cfg.Cache.CleanupIntervalMin > 0 {
		cfg.Cache.CleanupInterval = time.Duration(cfg.Cache.CleanupIntervalMin) * time.Minute
	}

	return cfg
}

// FindConfigFile searches for a configuration file in common locations.
// Returns the path to the first file found, or empty string if none found.
func FindConfigFile() string {
	// Check environment variable first
	if envPath := os.Getenv(EnvConfigFile); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	// Search paths in order of priority
	searchPaths := []string{
		DefaultConfigFileName,                                  // Current directory
		filepath.Join(".", DefaultConfigFileName),              // Explicit current directory
		filepath.Join("/etc/apt-proxy", DefaultConfigFileName), // System config
	}

	// Add home directory config if HOME is set
	if home := os.Getenv("HOME"); home != "" {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".config", "apt-proxy", DefaultConfigFileName),
			filepath.Join(home, ".apt-proxy.yaml"),
		)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// MergeConfigs merges two configurations, with values from 'override' taking precedence.
// Zero values in 'override' do not override values in 'base'.
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

	return &result
}

// ParseFlagsWithConfigFile parses command-line flags and optionally loads
// configuration from a YAML file. Priority: CLI > ENV > Config File > Default.
func ParseFlagsWithConfigFile() (*Config, error) {
	flags := flag.NewFlagSet("apt-proxy", flag.ContinueOnError)

	// Define flags (for CLI compatibility and help text)
	flags.String("host", DefaultHost, "the host to bind to")
	flags.String("port", DefaultPort, "the port to bind to")
	flags.String("mode", distro.LINUX_ALL_DISTROS,
		"select the mode of system to cache: all / ubuntu / ubuntu-ports / debian / centos / alpine")
	flags.Bool("debug", false, "whether to output debugging logging")
	flags.String("cachedir", DefaultCacheDir, "the dir to store cache data in")
	flags.String("ubuntu", "", "the ubuntu mirror for fetching packages")
	flags.String("ubuntu-ports", "", "the ubuntu ports mirror for fetching packages")
	flags.String("debian", "", "the debian mirror for fetching packages")
	flags.String("centos", "", "the centos mirror for fetching packages")
	flags.String("alpine", "", "the alpine mirror for fetching packages")

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

	// Config file flag
	flags.String("config", "", "path to YAML configuration file")

	// Security configuration flags
	flags.String("api-key", "", "API key for protected endpoints")

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
	cliConfig := buildCLIConfig(flags)

	// Merge configurations: file config as base, CLI/ENV as override
	config := MergeConfigs(fileConfig, cliConfig)

	// Apply defaults for any remaining unset values
	config = applyDefaults(config)

	// Update global state
	if err := UpdateGlobalState(config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return config, nil
}

// buildCLIConfig builds a Config from CLI flags and environment variables.
func buildCLIConfig(flags *flag.FlagSet) *Config {
	host := configutil.ResolveString(flags, "host", EnvHost, "", true)
	port := configutil.ResolveString(flags, "port", EnvPort, "", true)
	debug := configutil.ResolveBool(flags, "debug", EnvDebug, false)
	cacheDir := configutil.ResolveString(flags, "cachedir", EnvCacheDir, "", true)

	// Resolve mode
	modeName, _ := configutil.ResolveEnum(flags, "mode", EnvMode, "", allowedModes, false)

	// Resolve mirror configurations
	ubuntu := configutil.ResolveString(flags, "ubuntu", EnvUbuntu, "", true)
	ubuntuPorts := configutil.ResolveString(flags, "ubuntu-ports", EnvUbuntuPorts, "", true)
	debian := configutil.ResolveString(flags, "debian", EnvDebian, "", true)
	centos := configutil.ResolveString(flags, "centos", EnvCentOS, "", true)
	alpine := configutil.ResolveString(flags, "alpine", EnvAlpine, "", true)

	// Resolve cache configurations
	cacheMaxSizeGB := configutil.ResolveInt64(flags, "cache-max-size", EnvCacheMaxSize, 0, true)
	cacheTTLHours := configutil.ResolveInt(flags, "cache-ttl", EnvCacheTTL, 0, true)
	cacheCleanupIntervalMin := configutil.ResolveInt(flags, "cache-cleanup-interval", EnvCacheCleanupInterval, 0, true)

	// Resolve TLS configurations
	tlsEnabled := configutil.ResolveBool(flags, "tls", EnvTLSEnabled, false)
	tlsCertFile := configutil.ResolveString(flags, "tls-cert", EnvTLSCertFile, "", true)
	tlsKeyFile := configutil.ResolveString(flags, "tls-key", EnvTLSKeyFile, "", true)

	// Resolve security configurations
	apiKey := configutil.ResolveString(flags, "api-key", EnvAPIKey, "", true)
	enableAPIAuth := configutil.ResolveBool(flags, "api-key", EnvEnableAPIAuth, false)
	if apiKey != "" {
		enableAPIAuth = true
	}

	config := &Config{
		Debug:    debug,
		CacheDir: cacheDir,
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
			APIKey:        apiKey,
			EnableAPIAuth: enableAPIAuth,
		},
	}

	// Set mode if specified
	if modeName != "" {
		config.Mode = ModeToInt(modeName)
	}

	// Set listen address if host or port specified
	if host != "" || port != "" {
		if host == "" {
			host = DefaultHost
		}
		if port == "" {
			port = DefaultPort
		}
		listenAddr, err := mirrors.BuildListenAddress(host, port)
		if err != nil {
			config.Listen = fmt.Sprintf("%s:%s", host, port)
		} else {
			config.Listen = listenAddr
		}
	}

	return config
}

// applyDefaults applies default values to any unset configuration fields.
func applyDefaults(config *Config) *Config {
	if config == nil {
		config = &Config{}
	}

	if config.CacheDir == "" {
		config.CacheDir = DefaultCacheDir
	}

	if config.Listen == "" {
		listenAddr, err := mirrors.BuildListenAddress(DefaultHost, DefaultPort)
		if err != nil {
			config.Listen = fmt.Sprintf("%s:%s", DefaultHost, DefaultPort)
		} else {
			config.Listen = listenAddr
		}
	}

	if config.Mode == 0 {
		config.Mode = distro.TYPE_LINUX_ALL_DISTROS
	}

	// Apply cache defaults
	if config.Cache.MaxSize == 0 {
		config.Cache.MaxSize = httpcache.DefaultMaxCacheSize
	}
	if config.Cache.TTL == 0 {
		config.Cache.TTL = httpcache.DefaultCacheTTL
	}
	if config.Cache.CleanupInterval == 0 {
		config.Cache.CleanupInterval = httpcache.DefaultCleanupInterval
	}

	return config
}

// GetConfigFilePaths returns a slice of paths searched for configuration files.
// Useful for debugging and logging.
func GetConfigFilePaths() []string {
	paths := []string{
		DefaultConfigFileName,
		filepath.Join("/etc/apt-proxy", DefaultConfigFileName),
	}

	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths,
			filepath.Join(home, ".config", "apt-proxy", DefaultConfigFileName),
			filepath.Join(home, ".apt-proxy.yaml"),
		)
	}

	return paths
}

// IsConfigFileProvided checks if a config file path was explicitly provided
// via CLI flag or environment variable.
func IsConfigFileProvided() bool {
	// Check environment variable
	if os.Getenv(EnvConfigFile) != "" {
		return true
	}

	// Check CLI args for -config or --config
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-config") || strings.HasPrefix(arg, "--config") {
			return true
		}
	}

	return false
}
