package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/internal/state"
	"github.com/soulteary/cli-kit/configutil"
	httpcache "github.com/soulteary/httpcache-kit"
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
	config, _ := buildCLIConfig(flags, DefaultHost, DefaultPort, DefaultCacheDir, DefaultCacheMaxSizeGB, DefaultCacheTTLHours, DefaultCacheCleanupIntervalMin)

	// Set mode (buildCLIConfig may have set it, but we ensure it's set here with validated value)
	config.Mode = ModeToInt(modeName)

	// Apply defaults for cache if values are 0
	if config.Cache.MaxSize == 0 {
		config.Cache.MaxSize = httpcache.DefaultMaxCacheSize
	}
	if config.Cache.TTL == 0 {
		config.Cache.TTL = httpcache.DefaultCacheTTL
	}
	if config.Cache.CleanupInterval == 0 {
		config.Cache.CleanupInterval = httpcache.DefaultCleanupInterval
	}

	// Set listen address if not already set
	if config.Listen == "" {
		host := configutil.ResolveString(flags, "host", EnvHost, DefaultHost, true)
		port := configutil.ResolveString(flags, "port", EnvPort, DefaultPort, true)
		listenAddr, err := mirrors.BuildListenAddress(host, port)
		if err != nil {
			config.Listen = fmt.Sprintf("%s:%s", host, port)
		} else {
			config.Listen = listenAddr
		}
	}

	// Update global state
	if err := UpdateGlobalState(config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return config, nil
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

	// Validate listen address format (host:port or :port)
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", config.Listen, err)
	}

	// Ensure cache directory exists and is writable
	if err := os.MkdirAll(config.CacheDir, 0750); err != nil {
		return fmt.Errorf("cache directory %q cannot be created: %w", config.CacheDir, err)
	}
	// Check writable by creating a temp file
	testFile := filepath.Join(config.CacheDir, ".apt-proxy-write-test")
	if err := os.WriteFile(testFile, nil, 0600); err != nil {
		return fmt.Errorf("cache directory %q is not writable: %w", config.CacheDir, err)
	}
	_ = os.Remove(testFile)

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
		APIKey                string   `yaml:"api_key"`
		EnableAPIAuth         bool     `yaml:"enable_api_auth"`
		APIRateLimitPerMinute int      `yaml:"api_rate_limit_per_minute"`
		TrustedProxies        []string `yaml:"trusted_proxies"`
	} `yaml:"security"`

	Mode                string `yaml:"mode"`
	DistributionsConfig string `yaml:"distributions_config"`
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
			APIKey:                yamlCfg.Security.APIKey,
			EnableAPIAuth:         yamlCfg.Security.EnableAPIAuth,
			APIRateLimitPerMinute: yamlCfg.Security.APIRateLimitPerMinute,
			TrustedProxies:        append([]string(nil), yamlCfg.Security.TrustedProxies...),
		},
		DistributionsConfigPath: yamlCfg.DistributionsConfig,
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
		cleaned := filepath.Clean(envPath)
		if _, err := os.Stat(cleaned); err == nil { // #nosec G304 -- operator-controlled config path
			return cleaned
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
		cleaned := filepath.Clean(path)
		if _, err := os.Stat(cleaned); err == nil { // #nosec G304 -- well-known config search paths
			return cleaned
		}
	}

	return ""
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
	// UpstreamKeepAlive: override (CLI/ENV) wins when merging
	result.UpstreamKeepAlive = override.UpstreamKeepAlive

	return &result
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

	// Apply defaults for any remaining unset values
	config = applyDefaults(config)

	// Update global state
	if err := UpdateGlobalState(config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return config, nil
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
	// a usable bool value. Setting APIKey still implies enable=true below.
	enableAPIAuth := configutil.ResolveBool(flags, "enable-api-auth", EnvEnableAPIAuth, false)
	if apiKey != "" {
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
		listenAddr, err := mirrors.BuildListenAddress(host, port)
		if err != nil {
			config.Listen = fmt.Sprintf("%s:%s", host, port)
		} else {
			config.Listen = listenAddr
		}
	}

	return config, ex
}
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
		config.Mode = distro.TypeAllDistros
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
