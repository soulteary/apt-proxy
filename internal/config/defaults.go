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
	EnvAPIKey                = "APT_PROXY_API_KEY" // #nosec G101 -- env var name, not a credential
	EnvEnableAPIAuth         = "APT_PROXY_ENABLE_API_AUTH"
	EnvAPIRateLimitPerMinute = "APT_PROXY_API_RATE_LIMIT_PER_MINUTE"
	EnvTrustedProxies        = "APT_PROXY_TRUSTED_PROXIES"

	// Configuration file environment variable
	EnvConfigFile = "APT_PROXY_CONFIG_FILE"

	// Upstream transport
	EnvUpstreamKeepAlive = "APT_PROXY_UPSTREAM_KEEP_ALIVE"

	// Distributions configuration (distributions.yaml) path
	EnvDistributionsConfig = "APT_PROXY_DISTRIBUTIONS_CONFIG"

	// Storage backend selection: "disk" (default) or "s3"
	EnvStorageBackend = "APT_PROXY_STORAGE_BACKEND"

	// S3 storage backend environment variables. Credentials default to the
	// canonical AWS_* names so existing IAM tooling (instance role, IRSA,
	// shared credentials) keeps working without explicit configuration.
	EnvS3Endpoint     = "APT_PROXY_S3_ENDPOINT"
	EnvS3Region       = "APT_PROXY_S3_REGION"
	EnvS3Bucket       = "APT_PROXY_S3_BUCKET"
	EnvS3Prefix       = "APT_PROXY_S3_PREFIX"
	EnvS3AccessKey    = "APT_PROXY_S3_ACCESS_KEY"    // #nosec G101 -- env var name, not a credential
	EnvS3SecretKey    = "APT_PROXY_S3_SECRET_KEY"    // #nosec G101 -- env var name, not a credential
	EnvS3SessionToken = "APT_PROXY_S3_SESSION_TOKEN" // #nosec G101 -- env var name, not a credential
	EnvS3UseSSL       = "APT_PROXY_S3_USE_SSL"
	EnvS3UsePathStyle = "APT_PROXY_S3_USE_PATH_STYLE"
	EnvS3InlineMaxMB  = "APT_PROXY_S3_INLINE_MAX_MB"
	EnvS3TempDir      = "APT_PROXY_S3_TEMP_DIR"
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

	// Default API rate limit: requests per IP per minute (0 = disabled)
	DefaultAPIRateLimitPerMinute = 60

	// Default storage backend (local disk preserves prior behaviour).
	DefaultStorageBackend = "disk"
	// Default in-memory write threshold for the S3 backend, in mebibytes.
	// Above this size a spill to TempDir occurs before PutObject runs.
	DefaultS3InlineMaxMB = 32
	// Default object-key prefix for the S3 backend. The trailing slash is
	// significant: it keeps every cached body/header inside a single
	// "folder" so the bucket can be shared with other tooling.
	DefaultS3Prefix = "apt-proxy/"
)

// Environment variable names for logging configuration. The historical names
// (LOG_LEVEL / LOG_FORMAT) collide with other tooling, so the canonical names
// are now prefixed with APT_PROXY_. The unprefixed variants are read as a
// fallback (see ResolveLogLevelEnv) so existing deployments don't break.
const (
	EnvLogLevel        = "APT_PROXY_LOG_LEVEL"
	EnvLogFormat       = "APT_PROXY_LOG_FORMAT"
	EnvLogLevelLegacy  = "LOG_LEVEL"
	EnvLogFormatLegacy = "LOG_FORMAT"
)
