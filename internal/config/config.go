// Package config provides configuration management for apt-proxy.
package config

import "time"

// Config holds all application configuration
type Config struct {
	Debug    bool
	CacheDir string
	Mode     int
	Listen   string
	Mirrors  MirrorConfig
	Cache    CacheConfig
	TLS      TLSConfig
}

// TLSConfig holds TLS/HTTPS configuration
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled
	Enabled bool
	// CertFile is the path to the TLS certificate file
	CertFile string
	// KeyFile is the path to the TLS private key file
	KeyFile string
}

// MirrorConfig holds mirror-specific configuration
type MirrorConfig struct {
	Ubuntu      string
	UbuntuPorts string
	Debian      string
	CentOS      string
	Alpine      string
}

// CacheConfig holds cache-specific configuration
type CacheConfig struct {
	// MaxSize is the maximum cache size in bytes (default: 10GB)
	MaxSize int64
	// TTL is the time-to-live for cached items (default: 7 days)
	TTL time.Duration
	// CleanupInterval is the interval between cleanup runs (default: 1 hour)
	CleanupInterval time.Duration
}
