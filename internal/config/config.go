// Package config provides configuration management for apt-proxy.
package config

import "time"

// Config holds all application configuration
type Config struct {
	Debug    bool           `yaml:"debug"`
	CacheDir string         `yaml:"cache_dir"`
	Mode     int            `yaml:"mode"`
	Listen   string         `yaml:"listen"`
	Mirrors  MirrorConfig   `yaml:"mirrors"`
	Cache    CacheConfig    `yaml:"cache"`
	TLS      TLSConfig      `yaml:"tls"`
	Security SecurityConfig `yaml:"security"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	// APIKey is the key required for accessing protected API endpoints.
	// If empty, API authentication is disabled.
	APIKey string `yaml:"api_key"`
	// EnableAPIAuth enables API authentication when APIKey is set.
	EnableAPIAuth bool `yaml:"enable_api_auth"`
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

// CacheConfig holds cache-specific configuration
type CacheConfig struct {
	// MaxSize is the maximum cache size in bytes (default: 10GB)
	MaxSize int64 `yaml:"max_size"`
	// MaxSizeGB is an alternative way to specify max size in GB for YAML config
	MaxSizeGB int64 `yaml:"max_size_gb"`
	// TTL is the time-to-live for cached items (default: 7 days)
	TTL time.Duration `yaml:"ttl"`
	// TTLHours is an alternative way to specify TTL in hours for YAML config
	TTLHours int `yaml:"ttl_hours"`
	// CleanupInterval is the interval between cleanup runs (default: 1 hour)
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	// CleanupIntervalMin is an alternative way to specify cleanup interval in minutes for YAML config
	CleanupIntervalMin int `yaml:"cleanup_interval_min"`
}
