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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/metalink"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
	"gopkg.in/yaml.v3"
)

// YAMLConfig represents the YAML configuration file structure.
// It uses a more user-friendly structure that maps to the internal Config.
type YAMLConfig struct {
	Metalink       *metalink.Config       `yaml:"metalink"`
	SharedCache    *sharedcache.Config    `yaml:"shared_cache"`
	Resources      ResourceConfig         `yaml:"resources"`
	MirrorHealth   *mirrorhealth.Config   `yaml:"mirror_health"`
	UpstreamAccess *access.UpstreamConfig `yaml:"upstream_access"`
	Server         struct {
		Host  string `yaml:"host"`
		Port  string `yaml:"port"`
		Debug bool   `yaml:"debug"`
	} `yaml:"server"`

	Cache struct {
		Integrity          string `yaml:"integrity"`
		Dir                string `yaml:"dir"`
		MaxSizeGB          int64  `yaml:"max_size_gb"`
		TTLHours           int    `yaml:"ttl_hours"`
		CleanupIntervalMin int    `yaml:"cleanup_interval_min"`
	} `yaml:"cache"`

	Mirrors struct {
		Ubuntu         string `yaml:"ubuntu"`
		UbuntuPorts    string `yaml:"ubuntu_ports"`
		Debian         string `yaml:"debian"`
		DebianSecurity string `yaml:"debian_security"`
		CentOS         string `yaml:"centos"`
		Alpine         string `yaml:"alpine"`
	} `yaml:"mirrors"`

	TLS struct {
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`

	Security struct {
		DownloadAllowCIDRs []string `yaml:"download_allow_cidrs"`
		DownloadDenyCIDRs  []string `yaml:"download_deny_cidrs"`
		APIKey             string   `yaml:"api_key"`
		// Pointer preserves the distinction between an omitted value (API key
		// implies authentication) and an explicit false override.
		EnableAPIAuth *bool `yaml:"enable_api_auth"`
		// Pointer keeps DefaultAPIRateLimitPerMinute when the key is omitted;
		// an explicit 0 disables the limit.
		APIRateLimitPerMinute *int     `yaml:"api_rate_limit_per_minute"`
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
			// Pointer keeps the default of true when the key is omitted; an
			// explicit false selects plain HTTP.
			UseSSL       *bool  `yaml:"use_ssl"`
			UsePathStyle bool   `yaml:"use_path_style"`
			InlineMaxMB  int64  `yaml:"inline_max_mb"`
			TempDir      string `yaml:"temp_dir"`
		} `yaml:"s3"`
	} `yaml:"storage"`

	Mode                modeList `yaml:"mode"`
	DistributionsConfig string   `yaml:"distributions_config"`
	Passthrough         []string `yaml:"passthrough"`

	// UpstreamKeepAlive enables HTTP keep-alive to upstream mirrors.
	// Pointer to distinguish "user did not set" (nil → leave to defaults
	// or CLI/ENV) from "user explicitly set false" (disable keep-alive).
	Download          DownloadConfig `yaml:"download"`
	UpstreamKeepAlive *bool          `yaml:"upstream_keep_alive"`
}

// modeList is the YAML form of the mode setting: one distribution ID, or a
// list of IDs. A value may also hold a comma-separated list, as --mode and
// APT_PROXY_MODE do, so that a ${VAR} reference can supply a set.
type modeList []string

// UnmarshalYAML accepts a scalar or a sequence of scalars.
func (m *modeList) UnmarshalYAML(node *yaml.Node) error {
	var values []string
	switch node.Kind {
	case yaml.ScalarNode:
		values = []string{node.Value}
	case yaml.SequenceNode:
		if err := node.Decode(&values); err != nil {
			return err
		}
	default:
		// A TypeError is collected with the other field errors of the
		// document instead of ending the decoding at this one.
		return &yaml.TypeError{Errors: []string{
			fmt.Sprintf("line %d: mode must be a distribution ID or a list of distribution IDs", node.Line),
		}}
	}
	*m = splitCommaList(strings.Join(values, ","))
	return nil
}

// LoadConfigFile loads configuration from a YAML file.
// It returns nil if the file does not exist. The file must be a single YAML
// document that uses known fields only, and every ${VAR} reference in a value
// must resolve; an empty file yields the defaults.
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
	expandedData := expandConfigEnv(string(data))

	// A reference that is still present in a value names an unset variable.
	// Fail instead of using its literal text as the setting, which would for
	// example make "${APT_PROXY_API_KEY}" the working API key.
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(expandedData), &document); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	if names := unresolvedEnvRefs(&document); len(names) > 0 {
		return nil, fmt.Errorf("unresolved environment variables: %s (set them or use the ${NAME:-default} form)",
			strings.Join(names, ", "))
	}

	var yamlCfg YAMLConfig
	decoder := yaml.NewDecoder(strings.NewReader(expandedData))
	// Reject misspelled settings rather than silently dropping the policy
	// they were meant to set (for example, download_allow_cidr instead of
	// download_allow_cidrs).
	decoder.KnownFields(true)
	if err := decoder.Decode(&yamlCfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to parse config file: %w", trimUnknownFieldTypes(err))
	}
	// An empty file is valid and means no configuration. Additional documents,
	// including empty ones, must not hide configuration that would be ignored.
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
		return nil, fmt.Errorf("failed to parse config file: expected a single YAML document")
	}

	return yamlConfigToConfig(&yamlCfg), nil
}

// trimUnknownFieldTypes removes the Go type name that yaml.v3 appends to an
// unknown-field error. For the anonymous section structs of YAMLConfig that
// name is the whole struct definition; the line and field name are enough.
func trimUnknownFieldTypes(err error) error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		for i, msg := range typeErr.Errors {
			if field, _, found := strings.Cut(msg, " not found in type "); found {
				typeErr.Errors[i] = field + " not found"
			}
		}
	}
	return err
}

// envRefPattern matches ${NAME} or ${NAME:-default} where NAME is a typical
// shell-safe identifier (letters/digits/underscore, starting with a letter or
// underscore). Bare $NAME is NOT recognised; literal `$` characters in the
// YAML (e.g. inside passwords) are passed through untouched.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandConfigEnv performs a constrained environment-variable expansion on
// the YAML source. Unlike os.ExpandEnv it:
//   - only honours ${VAR} (and ${VAR:-default}); never bare $VAR.
//   - leaves an unset ${VAR} reference unchanged; LoadConfigFile rejects the
//     ones that remain in a value (see unresolvedEnvRefs).
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

// unresolvedEnvRefs returns the sorted, de-duplicated variable names of the
// references found in the scalars of an expanded document. Comments are not
// scalars, so a reference that only appears in a comment is not reported.
func unresolvedEnvRefs(document *yaml.Node) []string {
	var names []string
	var walk func(node *yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode {
			for _, groups := range envRefPattern.FindAllStringSubmatch(node.Value, -1) {
				names = append(names, groups[1])
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(document)
	slices.Sort(names)
	return slices.Compact(names)
}

// yamlConfigToConfig converts a YAMLConfig to the internal Config structure.
func yamlConfigToConfig(yamlCfg *YAMLConfig) *Config {
	enableAPIAuth := strings.TrimSpace(yamlCfg.Security.APIKey) != ""
	if yamlCfg.Security.EnableAPIAuth != nil {
		enableAPIAuth = *yamlCfg.Security.EnableAPIAuth
	}

	// Omitted keys keep the CLI defaults: the merge replaces a file value
	// only with a flag or environment variable that was set explicitly.
	apiRateLimitPerMinute := DefaultAPIRateLimitPerMinute
	if yamlCfg.Security.APIRateLimitPerMinute != nil {
		apiRateLimitPerMinute = *yamlCfg.Security.APIRateLimitPerMinute
	}
	s3UseSSL := true
	if yamlCfg.Storage.S3.UseSSL != nil {
		s3UseSSL = *yamlCfg.Storage.S3.UseSSL
	}

	cfg := &Config{
		Metalink:     yamlCfg.Metalink,
		SharedCache:  yamlCfg.SharedCache,
		MirrorHealth: yamlCfg.MirrorHealth,
		Debug:        yamlCfg.Server.Debug,
		CacheDir:     yamlCfg.Cache.Dir,
		Mirrors: MirrorConfig{
			Ubuntu:         yamlCfg.Mirrors.Ubuntu,
			UbuntuPorts:    yamlCfg.Mirrors.UbuntuPorts,
			Debian:         yamlCfg.Mirrors.Debian,
			DebianSecurity: yamlCfg.Mirrors.DebianSecurity,
			CentOS:         yamlCfg.Mirrors.CentOS,
			Alpine:         yamlCfg.Mirrors.Alpine,
		},
		Cache: CacheConfig{
			Integrity:          yamlCfg.Cache.Integrity,
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
			DownloadAllowCIDRs:    slices.Clone(yamlCfg.Security.DownloadAllowCIDRs),
			DownloadDenyCIDRs:     slices.Clone(yamlCfg.Security.DownloadDenyCIDRs),
			APIKey:                yamlCfg.Security.APIKey,
			EnableAPIAuth:         enableAPIAuth,
			APIRateLimitPerMinute: apiRateLimitPerMinute,
			TrustedProxies:        append([]string(nil), yamlCfg.Security.TrustedProxies...),
		},
		Resources:      yamlCfg.Resources,
		Download:       yamlCfg.Download,
		UpstreamAccess: yamlCfg.UpstreamAccess,
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
				UseSSL:       s3UseSSL,
				UsePathStyle: yamlCfg.Storage.S3.UsePathStyle,
				InlineMaxMB:  yamlCfg.Storage.S3.InlineMaxMB,
				TempDir:      yamlCfg.Storage.S3.TempDir,
			},
		},
		DistributionsConfigPath: yamlCfg.DistributionsConfig,
		Passthrough:             append([]string(nil), yamlCfg.Passthrough...),
	}

	// Apply UpstreamKeepAlive: default to true (matches CLI default) so the
	// merged base does not silently flip a CLI default of true to YAML's
	// boolean zero value. Only an explicit YAML false disables keep-alive.
	if yamlCfg.UpstreamKeepAlive != nil {
		cfg.UpstreamKeepAlive = *yamlCfg.UpstreamKeepAlive
	} else {
		cfg.UpstreamKeepAlive = true
	}

	// The mode is kept as written: its IDs are resolved against the
	// distribution registry at startup (ResolveMode).
	cfg.Modes = yamlCfg.Mode

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
