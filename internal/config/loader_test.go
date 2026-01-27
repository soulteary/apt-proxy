package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFile(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test-config.yaml")

	configContent := `
server:
  host: "127.0.0.1"
  port: "8080"
  debug: true

cache:
  dir: "/var/cache/test"
  max_size_gb: 20
  ttl_hours: 48
  cleanup_interval_min: 30

mirrors:
  ubuntu: "https://mirrors.test.com/ubuntu"
  debian: "https://mirrors.test.com/debian"

tls:
  enabled: true
  cert_file: "/etc/ssl/cert.pem"
  key_file: "/etc/ssl/key.pem"

security:
  api_key: "test-api-key"
  enable_api_auth: true

mode: "ubuntu"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}

	if cfg == nil {
		t.Fatal("LoadConfigFile() returned nil config")
	}

	// Verify server config
	if cfg.Listen != "127.0.0.1:8080" {
		t.Errorf("expected Listen '127.0.0.1:8080', got '%s'", cfg.Listen)
	}
	if !cfg.Debug {
		t.Error("expected Debug to be true")
	}

	// Verify cache config
	if cfg.CacheDir != "/var/cache/test" {
		t.Errorf("expected CacheDir '/var/cache/test', got '%s'", cfg.CacheDir)
	}
	expectedMaxSize := int64(20 * 1024 * 1024 * 1024)
	if cfg.Cache.MaxSize != expectedMaxSize {
		t.Errorf("expected MaxSize %d, got %d", expectedMaxSize, cfg.Cache.MaxSize)
	}
	expectedTTL := 48 * time.Hour
	if cfg.Cache.TTL != expectedTTL {
		t.Errorf("expected TTL %v, got %v", expectedTTL, cfg.Cache.TTL)
	}
	expectedCleanup := 30 * time.Minute
	if cfg.Cache.CleanupInterval != expectedCleanup {
		t.Errorf("expected CleanupInterval %v, got %v", expectedCleanup, cfg.Cache.CleanupInterval)
	}

	// Verify mirrors config
	if cfg.Mirrors.Ubuntu != "https://mirrors.test.com/ubuntu" {
		t.Errorf("expected Ubuntu mirror 'https://mirrors.test.com/ubuntu', got '%s'", cfg.Mirrors.Ubuntu)
	}
	if cfg.Mirrors.Debian != "https://mirrors.test.com/debian" {
		t.Errorf("expected Debian mirror 'https://mirrors.test.com/debian', got '%s'", cfg.Mirrors.Debian)
	}

	// Verify TLS config
	if !cfg.TLS.Enabled {
		t.Error("expected TLS.Enabled to be true")
	}
	if cfg.TLS.CertFile != "/etc/ssl/cert.pem" {
		t.Errorf("expected TLS.CertFile '/etc/ssl/cert.pem', got '%s'", cfg.TLS.CertFile)
	}
	if cfg.TLS.KeyFile != "/etc/ssl/key.pem" {
		t.Errorf("expected TLS.KeyFile '/etc/ssl/key.pem', got '%s'", cfg.TLS.KeyFile)
	}

	// Verify security config
	if cfg.Security.APIKey != "test-api-key" {
		t.Errorf("expected Security.APIKey 'test-api-key', got '%s'", cfg.Security.APIKey)
	}
	if !cfg.Security.EnableAPIAuth {
		t.Error("expected Security.EnableAPIAuth to be true")
	}
}

func TestLoadConfigFile_NotFound(t *testing.T) {
	cfg, err := LoadConfigFile("/nonexistent/path/config.yaml")
	if err != nil {
		t.Errorf("LoadConfigFile() should not return error for missing file, got %v", err)
	}
	if cfg != nil {
		t.Error("LoadConfigFile() should return nil for missing file")
	}
}

func TestLoadConfigFile_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "invalid.yaml")

	invalidContent := `
server:
  host: [invalid yaml
  port: 8080
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfigFile(configPath)
	if err == nil {
		t.Error("LoadConfigFile() should return error for invalid YAML")
	}
}

func TestLoadConfigFile_EnvironmentExpansion(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "env-config.yaml")

	// Set environment variable
	os.Setenv("TEST_API_KEY", "secret-from-env")
	defer os.Unsetenv("TEST_API_KEY")

	configContent := `
security:
  api_key: "${TEST_API_KEY}"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}

	if cfg.Security.APIKey != "secret-from-env" {
		t.Errorf("expected APIKey 'secret-from-env', got '%s'", cfg.Security.APIKey)
	}
}

func TestMergeConfigs(t *testing.T) {
	base := &Config{
		Debug:    false,
		CacheDir: "/base/cache",
		Listen:   "0.0.0.0:3142",
		Mode:     1,
		Mirrors: MirrorConfig{
			Ubuntu: "https://base.ubuntu.com",
			Debian: "https://base.debian.org",
		},
		Cache: CacheConfig{
			MaxSize: 10 * 1024 * 1024 * 1024,
			TTL:     168 * time.Hour,
		},
	}

	override := &Config{
		Debug:    true, // Override
		CacheDir: "",   // Don't override (empty)
		Listen:   "127.0.0.1:8080",
		Mirrors: MirrorConfig{
			Ubuntu: "https://override.ubuntu.com", // Override
			// Debian not set, should keep base
		},
		Cache: CacheConfig{
			MaxSize: 20 * 1024 * 1024 * 1024, // Override
		},
		Security: SecurityConfig{
			APIKey: "new-key",
		},
	}

	result := MergeConfigs(base, override)

	if !result.Debug {
		t.Error("Debug should be overridden to true")
	}
	if result.CacheDir != "/base/cache" {
		t.Errorf("CacheDir should not be overridden, got '%s'", result.CacheDir)
	}
	if result.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen should be overridden, got '%s'", result.Listen)
	}
	if result.Mirrors.Ubuntu != "https://override.ubuntu.com" {
		t.Errorf("Ubuntu mirror should be overridden, got '%s'", result.Mirrors.Ubuntu)
	}
	if result.Mirrors.Debian != "https://base.debian.org" {
		t.Errorf("Debian mirror should be kept from base, got '%s'", result.Mirrors.Debian)
	}
	if result.Cache.MaxSize != 20*1024*1024*1024 {
		t.Errorf("Cache.MaxSize should be overridden, got %d", result.Cache.MaxSize)
	}
	if result.Cache.TTL != 168*time.Hour {
		t.Errorf("Cache.TTL should be kept from base, got %v", result.Cache.TTL)
	}
	if result.Security.APIKey != "new-key" {
		t.Errorf("Security.APIKey should be overridden, got '%s'", result.Security.APIKey)
	}
}

func TestMergeConfigs_NilInputs(t *testing.T) {
	config := &Config{Debug: true}

	// Base is nil
	result := MergeConfigs(nil, config)
	if result != config {
		t.Error("MergeConfigs(nil, config) should return config")
	}

	// Override is nil
	result = MergeConfigs(config, nil)
	if result != config {
		t.Error("MergeConfigs(config, nil) should return config")
	}
}

func TestApplyDefaults(t *testing.T) {
	// Test with nil config
	result := applyDefaults(nil)
	if result == nil {
		t.Fatal("applyDefaults(nil) should return non-nil config")
	}
	if result.CacheDir == "" {
		t.Error("CacheDir should have default value")
	}
	if result.Listen == "" {
		t.Error("Listen should have default value")
	}

	// Test with partial config
	partial := &Config{
		CacheDir: "/custom/cache",
		// Listen is empty
	}
	result = applyDefaults(partial)
	if result.CacheDir != "/custom/cache" {
		t.Error("CacheDir should not be overwritten")
	}
	if result.Listen == "" {
		t.Error("Listen should have default value")
	}
}

func TestGetConfigFilePaths(t *testing.T) {
	paths := GetConfigFilePaths()
	if len(paths) == 0 {
		t.Error("GetConfigFilePaths() should return at least one path")
	}

	// Check that apt-proxy.yaml is in the paths
	foundDefault := false
	for _, p := range paths {
		if filepath.Base(p) == DefaultConfigFileName {
			foundDefault = true
			break
		}
	}
	if !foundDefault {
		t.Errorf("paths should contain %s", DefaultConfigFileName)
	}
}

func TestFindConfigFile(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, DefaultConfigFileName)
	if err := os.WriteFile(configPath, []byte("debug: true"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Set environment variable to point to the temp config
	os.Setenv(EnvConfigFile, configPath)
	defer os.Unsetenv(EnvConfigFile)

	found := FindConfigFile()
	if found != configPath {
		t.Errorf("FindConfigFile() = %s, want %s", found, configPath)
	}
}

func TestIsConfigFileProvided(t *testing.T) {
	// Clear environment
	os.Unsetenv(EnvConfigFile)

	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Test with environment variable
	os.Setenv(EnvConfigFile, "/some/path")
	if !IsConfigFileProvided() {
		t.Error("IsConfigFileProvided() should return true when env var is set")
	}
	os.Unsetenv(EnvConfigFile)

	// Test with CLI flag
	os.Args = []string{"apt-proxy", "--config=/some/path"}
	if !IsConfigFileProvided() {
		t.Error("IsConfigFileProvided() should return true when CLI flag is set")
	}

	// Test without any config
	os.Args = []string{"apt-proxy"}
	if IsConfigFileProvided() {
		t.Error("IsConfigFileProvided() should return false when no config is specified")
	}
}

func TestYamlConfigToConfig_Defaults(t *testing.T) {
	yamlCfg := &YAMLConfig{}
	cfg := yamlConfigToConfig(yamlCfg)

	if cfg == nil {
		t.Fatal("yamlConfigToConfig should not return nil")
	}

	// Empty YAML config should result in zero values
	if cfg.Debug {
		t.Error("Debug should be false for empty config")
	}
	if cfg.CacheDir != "" {
		t.Error("CacheDir should be empty for empty config")
	}
}

func TestYamlConfigToConfig_HostPort(t *testing.T) {
	yamlCfg := &YAMLConfig{}
	yamlCfg.Server.Host = "192.168.1.1"
	yamlCfg.Server.Port = "9000"

	cfg := yamlConfigToConfig(yamlCfg)

	if cfg.Listen != "192.168.1.1:9000" {
		t.Errorf("expected Listen '192.168.1.1:9000', got '%s'", cfg.Listen)
	}
}

func TestYamlConfigToConfig_PartialHostPort(t *testing.T) {
	// Only host specified
	yamlCfg := &YAMLConfig{}
	yamlCfg.Server.Host = "10.0.0.1"

	cfg := yamlConfigToConfig(yamlCfg)
	if cfg.Listen != "10.0.0.1:"+DefaultPort {
		t.Errorf("expected Listen with default port, got '%s'", cfg.Listen)
	}

	// Only port specified
	yamlCfg2 := &YAMLConfig{}
	yamlCfg2.Server.Port = "8888"

	cfg2 := yamlConfigToConfig(yamlCfg2)
	if cfg2.Listen != DefaultHost+":8888" {
		t.Errorf("expected Listen with default host, got '%s'", cfg2.Listen)
	}
}
