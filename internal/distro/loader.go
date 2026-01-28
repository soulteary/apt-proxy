// Package distro provides distribution configuration loading from external files
package distro

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// DistributionConfig represents a distribution configuration loaded from YAML
type DistributionConfig struct {
	ID           string            `yaml:"id"`
	Name         string            `yaml:"name"`
	Type         int               `yaml:"type"`
	URLPattern   string            `yaml:"url_pattern"`
	BenchmarkURL string            `yaml:"benchmark_url"`
	GeoMirrorAPI string            `yaml:"geo_mirror_api,omitempty"`
	CacheRules   []CacheRuleConfig `yaml:"cache_rules"`
	Mirrors      MirrorListConfig  `yaml:"mirrors"`
	Aliases      map[string]string `yaml:"aliases,omitempty"`
}

// CacheRuleConfig represents a cache rule configuration
type CacheRuleConfig struct {
	Pattern      string `yaml:"pattern"`
	CacheControl string `yaml:"cache_control"`
	Rewrite      bool   `yaml:"rewrite"`
}

// MirrorListConfig represents mirror list configuration
type MirrorListConfig struct {
	Official []string `yaml:"official"`
	Custom   []string `yaml:"custom"`
}

// DistributionsConfig represents the root configuration structure
type DistributionsConfig struct {
	Distributions []DistributionConfig `yaml:"distributions"`
}

// Loader handles loading distribution configurations
type Loader struct {
	configPath string
	config     *DistributionsConfig
}

// NewLoader creates a new distribution configuration loader
func NewLoader(configPath string) *Loader {
	return &Loader{
		configPath: configPath,
	}
}

// Load loads distribution configurations from the configured file
func (l *Loader) Load() (*DistributionsConfig, error) {
	if l.configPath == "" {
		// Try default paths
		defaultPaths := []string{
			"./config/distributions.yaml",
			"./distributions.yaml",
			"/etc/apt-proxy/distributions.yaml",
			filepath.Join(os.Getenv("HOME"), ".config/apt-proxy/distributions.yaml"),
		}

		for _, path := range defaultPaths {
			if _, err := os.Stat(path); err == nil {
				l.configPath = path
				break
			}
		}
	}

	if l.configPath == "" {
		// No config file found, return nil to use built-in defaults
		return nil, nil
	}

	data, err := os.ReadFile(l.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read distribution config: %w", err)
	}

	var config DistributionsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse distribution config: %w", err)
	}

	// Validate and compile regex patterns
	for i := range config.Distributions {
		dist := &config.Distributions[i]
		if err := l.validateDistribution(dist); err != nil {
			return nil, fmt.Errorf("invalid distribution config for %s: %w", dist.ID, err)
		}
	}

	l.config = &config
	return &config, nil
}

// validateDistribution validates a distribution configuration
func (l *Loader) validateDistribution(dist *DistributionConfig) error {
	if dist.ID == "" {
		return fmt.Errorf("distribution ID is required")
	}
	if dist.Name == "" {
		return fmt.Errorf("distribution name is required")
	}
	if dist.URLPattern == "" {
		return fmt.Errorf("URL pattern is required")
	}
	if dist.BenchmarkURL == "" {
		return fmt.Errorf("benchmark URL is required")
	}

	// Validate URL pattern is a valid regex
	if _, err := regexp.Compile(dist.URLPattern); err != nil {
		return fmt.Errorf("invalid URL pattern regex: %w", err)
	}

	// Validate cache rule patterns
	for i, rule := range dist.CacheRules {
		if rule.Pattern == "" {
			return fmt.Errorf("cache rule %d: pattern is required", i)
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("cache rule %d: invalid pattern regex: %w", i, err)
		}
	}

	return nil
}

// GetConfig returns the loaded configuration
func (l *Loader) GetConfig() *DistributionsConfig {
	return l.config
}

// Reload reloads the configuration from file
func (l *Loader) Reload() (*DistributionsConfig, error) {
	return l.Load()
}
