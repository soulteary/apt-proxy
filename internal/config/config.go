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

// Package config provides configuration management for apt-proxy.
package config

import "time"

// Storage backend identifiers used by StorageConfig.Backend.
const (
	StorageBackendDisk = "disk"
	StorageBackendS3   = "s3"
)

// Config holds all application configuration
type Config struct {
	Debug                   bool           `yaml:"debug"`
	CacheDir                string         `yaml:"cache_dir"`
	Mode                    int            `yaml:"mode"`
	Listen                  string         `yaml:"listen"`
	Mirrors                 MirrorConfig   `yaml:"mirrors"`
	Cache                   CacheConfig    `yaml:"cache"`
	Storage                 StorageConfig  `yaml:"storage"`
	TLS                     TLSConfig      `yaml:"tls"`
	Security                SecurityConfig `yaml:"security"`
	DistributionsConfigPath string         `yaml:"distributions_config"`
	// UpstreamKeepAlive enables HTTP keep-alive to upstream mirrors (default true).
	UpstreamKeepAlive bool `yaml:"upstream_keep_alive"`
}

// StorageConfig selects and configures the cache storage backend.
// "disk" (default) keeps the cache on the local filesystem under CacheDir;
// "s3" puts every cached body/header into an S3-compatible bucket so that
// many apt-proxy instances can share a single cache pool.
type StorageConfig struct {
	// Backend selects which storage implementation to use.
	// Empty defaults to "disk" for backward compatibility.
	Backend string   `yaml:"backend"`
	S3      S3Config `yaml:"s3"`
}

// S3Config holds the S3-compatible-storage credentials and tunables.
// Endpoint, Bucket, AccessKey and SecretKey are required when Backend == "s3";
// the rest of the fields have sensible defaults.
type S3Config struct {
	// Endpoint is the host[:port] of the S3 service, e.g. "s3.amazonaws.com"
	// or "minio.example.com:9000".
	Endpoint string `yaml:"endpoint"`
	// Region is the AWS-style region; required for AWS S3, ignored by most
	// MinIO-flavoured services.
	Region string `yaml:"region"`
	// Bucket is the destination bucket. It must already exist.
	Bucket string `yaml:"bucket"`
	// Prefix is an optional sub-path inside the bucket (e.g. "apt-proxy/")
	// allowing several deployments to share one bucket safely.
	Prefix string `yaml:"prefix"`
	// AccessKey / SecretKey are the static IAM credentials.
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	// SessionToken is the optional STS session token.
	SessionToken string `yaml:"session_token"`
	// UseSSL enables HTTPS to the endpoint. Defaults to true.
	UseSSL bool `yaml:"use_ssl"`
	// UsePathStyle forces path-style URLs. MinIO/Ceph generally need this.
	UsePathStyle bool `yaml:"use_path_style"`
	// InlineMaxMB is the in-memory write-buffer threshold in mebibytes.
	// Writes whose total size exceeds this value spill to a temp file before
	// being uploaded. Zero falls back to the package default (32).
	InlineMaxMB int64 `yaml:"inline_max_mb"`
	// TempDir is where spilled writes are staged. Empty means os.TempDir().
	TempDir string `yaml:"temp_dir"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	// APIKey is the key required for accessing protected API endpoints.
	// If empty, API authentication is disabled.
	APIKey string `yaml:"api_key"`
	// EnableAPIAuth enables API authentication when APIKey is set.
	EnableAPIAuth bool `yaml:"enable_api_auth"`
	// APIRateLimitPerMinute limits API requests per IP per minute (0 = disabled). Default 60.
	APIRateLimitPerMinute int `yaml:"api_rate_limit_per_minute"`
	// TrustedProxies is the list of CIDR networks (e.g. "10.0.0.0/8") whose
	// X-Forwarded-For header is honored for the API rate-limit / IP-based
	// audit fields. Leave empty to ignore XFF entirely (default secure).
	TrustedProxies []string `yaml:"trusted_proxies"`
}

// TLSConfig holds TLS/HTTPS configuration
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled
	Enabled bool `yaml:"enabled"`
	// CertFile is the path to the TLS certificate file
	CertFile string `yaml:"cert_file"`
	// KeyFile is the path to the TLS private key file
	KeyFile string `yaml:"key_file"`
}

// MirrorConfig holds mirror-specific configuration
type MirrorConfig struct {
	Ubuntu      string `yaml:"ubuntu"`
	UbuntuPorts string `yaml:"ubuntu_ports"`
	Debian      string `yaml:"debian"`
	CentOS      string `yaml:"centos"`
	Alpine      string `yaml:"alpine"`
}

// CacheConfig holds cache-specific configuration.
//
// Only the *GB / *Hours / *Min fields are user-facing in YAML (via the
// human-friendly YAMLConfig.Cache.* schema). The raw byte / time.Duration
// fields are internal representations populated by the loader; they carry
// `yaml:"-"` so the struct does not falsely advertise YAML support for
// "max_size" / "ttl" / "cleanup_interval" etc.
type CacheConfig struct {
	// MaxSize is the maximum cache size in bytes (default: 10GB).
	// Internal representation; not read from YAML.
	MaxSize int64 `yaml:"-"`
	// MaxSizeGB is an alternative way to specify max size in GB for YAML config.
	// Not read from the top-level Config YAML; YAMLConfig.Cache.MaxSizeGB
	// is the user-facing knob.
	MaxSizeGB int64 `yaml:"-"`
	// TTL is the time-to-live for cached items (default: 7 days).
	// Internal representation; not read from YAML.
	TTL time.Duration `yaml:"-"`
	// TTLHours is an alternative way to specify TTL in hours for YAML config.
	// Not read from the top-level Config YAML; YAMLConfig.Cache.TTLHours
	// is the user-facing knob.
	TTLHours int `yaml:"-"`
	// CleanupInterval is the interval between cleanup runs (default: 1 hour).
	// Internal representation; not read from YAML.
	CleanupInterval time.Duration `yaml:"-"`
	// CleanupIntervalMin is an alternative way to specify cleanup interval in minutes for YAML config.
	// Not read from the top-level Config YAML; YAMLConfig.Cache.CleanupIntervalMin
	// is the user-facing knob.
	CleanupIntervalMin int `yaml:"-"`
}
