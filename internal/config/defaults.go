package config

// Environment variable names for configuration
const (
	EnvHost        = "APT_PROXY_HOST"
	EnvPort        = "APT_PROXY_PORT"
	EnvMode        = "APT_PROXY_MODE"
	EnvCacheDir    = "APT_PROXY_CACHEDIR"
	EnvDebug       = "APT_PROXY_DEBUG"
	EnvUbuntu      = "APT_PROXY_UBUNTU"
	EnvUbuntuPorts = "APT_PROXY_UBUNTU_PORTS"
	EnvDebian      = "APT_PROXY_DEBIAN"
	EnvCentOS      = "APT_PROXY_CENTOS"
	EnvAlpine      = "APT_PROXY_ALPINE"

	// Cache configuration environment variables
	EnvCacheMaxSize         = "APT_PROXY_CACHE_MAX_SIZE"
	EnvCacheTTL             = "APT_PROXY_CACHE_TTL"
	EnvCacheCleanupInterval = "APT_PROXY_CACHE_CLEANUP_INTERVAL"

	// TLS configuration environment variables
	EnvTLSEnabled  = "APT_PROXY_TLS_ENABLED"
	EnvTLSCertFile = "APT_PROXY_TLS_CERT"
	EnvTLSKeyFile  = "APT_PROXY_TLS_KEY"

	// Security configuration environment variables
	EnvAPIKey        = "APT_PROXY_API_KEY"
	EnvEnableAPIAuth = "APT_PROXY_ENABLE_API_AUTH"

	// Configuration file environment variable
	EnvConfigFile = "APT_PROXY_CONFIG_FILE"

	// Distributions configuration (distributions.yaml) path
	EnvDistributionsConfig = "APT_PROXY_DISTRIBUTIONS_CONFIG"
)

// Default configuration values
const (
	DefaultHost     = "0.0.0.0"
	DefaultPort     = "3142"
	DefaultCacheDir = "./.aptcache"

	// Default cache configuration values (as strings for flag parsing)
	DefaultCacheMaxSizeGB          = 10  // 10 GB
	DefaultCacheTTLHours           = 168 // 7 days
	DefaultCacheCleanupIntervalMin = 60  // 1 hour

	// Default configuration file paths (searched in order)
	DefaultConfigFileName = "apt-proxy.yaml"

	// Default async benchmark setting
	DefaultAsyncBenchmark = true // Enable async mirror benchmark by default for faster startup
)

// Environment variable names for logging configuration
const (
	EnvLogLevel  = "LOG_LEVEL"
	EnvLogFormat = "LOG_FORMAT"
)
