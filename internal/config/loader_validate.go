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

// Package config configuration validation and global state hand-off.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/soulteary/apt-proxy/internal/state"
)

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

	if config.Listen == "" {
		return fmt.Errorf("listen address must be specified")
	}

	// Validate listen address format (host:port or :port)
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", config.Listen, err)
	}

	// Validate storage backend selection and corresponding fields. The
	// CacheDir checks below only apply to the local-disk backend; S3 uses
	// remote object storage and shouldn't be tied to a writable local path.
	switch config.Storage.Backend {
	case "", StorageBackendDisk:
		if config.CacheDir == "" {
			return fmt.Errorf("cache directory must be specified")
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
	case StorageBackendS3:
		if config.Storage.S3.Endpoint == "" {
			return fmt.Errorf("S3 endpoint must be specified when storage backend is %q", StorageBackendS3)
		}
		if config.Storage.S3.Bucket == "" {
			return fmt.Errorf("S3 bucket must be specified when storage backend is %q", StorageBackendS3)
		}
		// Allow anonymous credentials for fully-public/shared buckets, but
		// loudly require both keys when one is supplied: an empty secret
		// against a populated access key almost always means a misconfig.
		if (config.Storage.S3.AccessKey != "") != (config.Storage.S3.SecretKey != "") {
			return fmt.Errorf("S3 access_key and secret_key must be set together")
		}
	default:
		return fmt.Errorf("unknown storage backend %q (expected %q or %q)",
			config.Storage.Backend, StorageBackendDisk, StorageBackendS3)
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
