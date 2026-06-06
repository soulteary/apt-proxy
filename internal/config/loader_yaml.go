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

// Package config YAML file loading and conversion.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

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

	Storage struct {
		Backend string `yaml:"backend"`
		S3      struct {
			Endpoint     string `yaml:"endpoint"`
			Region       string `yaml:"region"`
			Bucket       string `yaml:"bucket"`
			Prefix       string `yaml:"prefix"`
			AccessKey    string `yaml:"access_key"`
			SecretKey    string `yaml:"secret_key"`
			SessionToken string `yaml:"session_token"`
			UseSSL       bool   `yaml:"use_ssl"`
			UsePathStyle bool   `yaml:"use_path_style"`
			InlineMaxMB  int64  `yaml:"inline_max_mb"`
			TempDir      string `yaml:"temp_dir"`
		} `yaml:"s3"`
	} `yaml:"storage"`

	Mode                string `yaml:"mode"`
	DistributionsConfig string `yaml:"distributions_config"`

	// UpstreamKeepAlive enables HTTP keep-alive to upstream mirrors.
	// Pointer to distinguish "user did not set" (nil → leave to defaults
	// or CLI/ENV) from "user explicitly set false" (disable keep-alive).
	UpstreamKeepAlive *bool `yaml:"upstream_keep_alive"`
}

// LoadConfigFile loads configuration from a YAML file.
// It returns nil if the file does not exist.
//
// The path is operator-controlled: it comes from the --config CLI flag, the
// APT_PROXY_CONFIG_FILE environment variable, or FindConfigFile's well-known
// search list. Cleaning the path before reading mirrors what FindConfigFile
// already does and keeps gosec G304 narrow to this single, audited site.
func LoadConfigFile(path string) (*Config, error) {
	cleaned := filepath.Clean(path)
	data, err := os.ReadFile(cleaned) // #nosec G304 -- operator-controlled config path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist, not an error
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in the YAML content. We use the
	// strict ${VAR} form only (NOT bare $VAR) so that passwords / tokens
	// containing literal `$` aren't silently truncated by os.ExpandEnv.
	// An unset ${VAR} is left as-is so operators can spot the typo
	// instead of seeing a mysterious empty string.
	expandedData := expandConfigEnv(string(data))

	var yamlCfg YAMLConfig
	if err := yaml.Unmarshal([]byte(expandedData), &yamlCfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return yamlConfigToConfig(&yamlCfg), nil
}

// envRefPattern matches ${NAME} or ${NAME:-default} where NAME is a typical
// shell-safe identifier (letters/digits/underscore, starting with a letter or
// underscore). Bare $NAME is NOT recognised; literal `$` characters in the
// YAML (e.g. inside passwords) are passed through untouched.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandConfigEnv performs a constrained environment-variable expansion on
// the YAML source. Unlike os.ExpandEnv it:
//   - only honours ${VAR} (and ${VAR:-default}); never bare $VAR.
//   - leaves an unset ${VAR} reference unchanged so operators see the typo.
func expandConfigEnv(src string) string {
	return envRefPattern.ReplaceAllStringFunc(src, func(match string) string {
		groups := envRefPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		name := groups[1]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		// `${VAR:-default}` form: groups[2] is "" when the default branch
		// wasn't matched (no `:-` present); only fall back to the default
		// when the original match actually contained `:-`.
		if len(groups) >= 3 && len(match) > len("${"+name+"}") {
			return groups[2]
		}
		return match
	})
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
		Storage: StorageConfig{
			Backend: yamlCfg.Storage.Backend,
			S3: S3Config{
				Endpoint:     yamlCfg.Storage.S3.Endpoint,
				Region:       yamlCfg.Storage.S3.Region,
				Bucket:       yamlCfg.Storage.S3.Bucket,
				Prefix:       yamlCfg.Storage.S3.Prefix,
				AccessKey:    yamlCfg.Storage.S3.AccessKey,
				SecretKey:    yamlCfg.Storage.S3.SecretKey,
				SessionToken: yamlCfg.Storage.S3.SessionToken,
				UseSSL:       yamlCfg.Storage.S3.UseSSL,
				UsePathStyle: yamlCfg.Storage.S3.UsePathStyle,
				InlineMaxMB:  yamlCfg.Storage.S3.InlineMaxMB,
				TempDir:      yamlCfg.Storage.S3.TempDir,
			},
		},
		DistributionsConfigPath: yamlCfg.DistributionsConfig,
	}

	// Apply UpstreamKeepAlive: default to true (matches CLI default) so the
	// merged base does not silently flip a CLI default of true to YAML's
	// boolean zero value. Only an explicit YAML false disables keep-alive.
	if yamlCfg.UpstreamKeepAlive != nil {
		cfg.UpstreamKeepAlive = *yamlCfg.UpstreamKeepAlive
	} else {
		cfg.UpstreamKeepAlive = true
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
