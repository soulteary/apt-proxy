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
	_ = os.Setenv("TEST_API_KEY", "secret-from-env")
	defer func() { _ = os.Unsetenv("TEST_API_KEY") }()

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
	_ = os.Setenv(EnvConfigFile, configPath)
	defer func() { _ = os.Unsetenv(EnvConfigFile) }()

	found := FindConfigFile()
	if found != configPath {
		t.Errorf("FindConfigFile() = %s, want %s", found, configPath)
	}
}

func TestIsConfigFileProvided(t *testing.T) {
	// Clear environment
	_ = os.Unsetenv(EnvConfigFile)

	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Test with environment variable
	_ = os.Setenv(EnvConfigFile, "/some/path")
	if !IsConfigFileProvided() {
		t.Error("IsConfigFileProvided() should return true when env var is set")
	}
	_ = os.Unsetenv(EnvConfigFile)

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

func TestValidateConfig(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		if err := ValidateConfig(nil); err == nil {
			t.Error("ValidateConfig(nil) should return error")
		}
	})
	t.Run("empty CacheDir", func(t *testing.T) {
		cfg := &Config{Listen: "0.0.0.0:3142", CacheDir: ""}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("ValidateConfig with empty CacheDir should return error")
		}
	})
	t.Run("empty Listen", func(t *testing.T) {
		cfg := &Config{Listen: "", CacheDir: t.TempDir()}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("ValidateConfig with empty Listen should return error")
		}
	})
	t.Run("invalid Listen format", func(t *testing.T) {
		cfg := &Config{Listen: "not-host-port", CacheDir: t.TempDir()}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("ValidateConfig with invalid Listen should return error")
		}
	})
	t.Run("CacheDir is a file not a directory", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(f, nil, 0644); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Listen: "0.0.0.0:3142", CacheDir: f}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("ValidateConfig with CacheDir as file should return error")
		}
	})
	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{Listen: "0.0.0.0:3142", CacheDir: t.TempDir()}
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("ValidateConfig with valid config should succeed: %v", err)
		}
	})
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

// TestValidateConfig_S3Backend covers the storage-backend branch added for
// the S3 cache. CacheDir is intentionally left empty for s3-backend cases:
// the validator must accept that.
func TestValidateConfig_S3Backend(t *testing.T) {
	t.Run("missing endpoint", func(t *testing.T) {
		cfg := &Config{
			Listen: "0.0.0.0:3142",
			Storage: StorageConfig{
				Backend: StorageBackendS3,
				S3:      S3Config{Bucket: "b"},
			},
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("missing S3 endpoint should fail validation")
		}
	})
	t.Run("missing bucket", func(t *testing.T) {
		cfg := &Config{
			Listen: "0.0.0.0:3142",
			Storage: StorageConfig{
				Backend: StorageBackendS3,
				S3:      S3Config{Endpoint: "minio:9000"},
			},
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("missing S3 bucket should fail validation")
		}
	})
	t.Run("orphan access key", func(t *testing.T) {
		cfg := &Config{
			Listen: "0.0.0.0:3142",
			Storage: StorageConfig{
				Backend: StorageBackendS3,
				S3: S3Config{
					Endpoint:  "minio:9000",
					Bucket:    "b",
					AccessKey: "ak",
				},
			},
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("access_key without secret_key should fail validation")
		}
	})
	t.Run("orphan secret key", func(t *testing.T) {
		cfg := &Config{
			Listen: "0.0.0.0:3142",
			Storage: StorageConfig{
				Backend: StorageBackendS3,
				S3: S3Config{
					Endpoint:  "minio:9000",
					Bucket:    "b",
					SecretKey: "sk",
				},
			},
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("secret_key without access_key should fail validation")
		}
	})
	t.Run("valid s3 config without CacheDir", func(t *testing.T) {
		cfg := &Config{
			Listen: "0.0.0.0:3142",
			// CacheDir intentionally empty: not required for s3 backend.
			Storage: StorageConfig{
				Backend: StorageBackendS3,
				S3: S3Config{
					Endpoint:  "minio:9000",
					Bucket:    "apt-proxy",
					AccessKey: "ak",
					SecretKey: "sk",
				},
			},
		}
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("valid s3 config should pass validation: %v", err)
		}
	})
	t.Run("unknown backend", func(t *testing.T) {
		cfg := &Config{
			Listen:   "0.0.0.0:3142",
			CacheDir: t.TempDir(),
			Storage:  StorageConfig{Backend: "blob"},
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("unknown storage backend should fail validation")
		}
	})
}

func TestMergeConfigs_StorageFields(t *testing.T) {
	base := &Config{
		Storage: StorageConfig{
			Backend: StorageBackendDisk,
			S3: S3Config{
				Prefix:      "old/",
				InlineMaxMB: 16,
				UseSSL:      false,
			},
		},
	}
	override := &Config{
		Storage: StorageConfig{
			Backend: StorageBackendS3,
			S3: S3Config{
				Endpoint:    "minio:9000",
				Bucket:      "apt-proxy",
				Prefix:      "new/",
				AccessKey:   "ak",
				SecretKey:   "sk",
				InlineMaxMB: 64,
				UseSSL:      true,
			},
		},
	}
	got := MergeConfigs(base, override)
	if got.Storage.Backend != StorageBackendS3 {
		t.Errorf("Backend should be overridden to s3, got %q", got.Storage.Backend)
	}
	if got.Storage.S3.Endpoint != "minio:9000" {
		t.Errorf("Endpoint should be overridden, got %q", got.Storage.S3.Endpoint)
	}
	if got.Storage.S3.Prefix != "new/" {
		t.Errorf("Prefix should be overridden, got %q", got.Storage.S3.Prefix)
	}
	if got.Storage.S3.InlineMaxMB != 64 {
		t.Errorf("InlineMaxMB should be overridden, got %d", got.Storage.S3.InlineMaxMB)
	}
	if !got.Storage.S3.UseSSL {
		t.Error("UseSSL should be overridden to true")
	}
}

func TestYamlConfigToConfig_StorageS3(t *testing.T) {
	yamlCfg := &YAMLConfig{}
	yamlCfg.Storage.Backend = StorageBackendS3
	yamlCfg.Storage.S3.Endpoint = "minio:9000"
	yamlCfg.Storage.S3.Region = "us-east-1"
	yamlCfg.Storage.S3.Bucket = "apt-proxy"
	yamlCfg.Storage.S3.Prefix = "cache/"
	yamlCfg.Storage.S3.AccessKey = "ak"
	yamlCfg.Storage.S3.SecretKey = "sk"
	yamlCfg.Storage.S3.UseSSL = true
	yamlCfg.Storage.S3.UsePathStyle = true
	yamlCfg.Storage.S3.InlineMaxMB = 8

	cfg := yamlConfigToConfig(yamlCfg)
	if cfg.Storage.Backend != StorageBackendS3 {
		t.Errorf("Backend mismatch: %q", cfg.Storage.Backend)
	}
	if cfg.Storage.S3.Endpoint != "minio:9000" || cfg.Storage.S3.Region != "us-east-1" {
		t.Errorf("endpoint/region mismatch: %+v", cfg.Storage.S3)
	}
	if cfg.Storage.S3.Bucket != "apt-proxy" || cfg.Storage.S3.Prefix != "cache/" {
		t.Errorf("bucket/prefix mismatch: %+v", cfg.Storage.S3)
	}
	if cfg.Storage.S3.AccessKey != "ak" || cfg.Storage.S3.SecretKey != "sk" {
		t.Error("access/secret key not propagated")
	}
	if !cfg.Storage.S3.UseSSL || !cfg.Storage.S3.UsePathStyle {
		t.Error("use_ssl / use_path_style not propagated")
	}
	if cfg.Storage.S3.InlineMaxMB != 8 {
		t.Errorf("InlineMaxMB mismatch: %d", cfg.Storage.S3.InlineMaxMB)
	}
}

func TestApplyDefaults_S3Backend(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{
			Backend: StorageBackendS3,
			S3: S3Config{
				Endpoint: "minio:9000",
				Bucket:   "apt-proxy",
			},
		},
	}
	got := applyDefaults(cfg)
	if got.Storage.S3.Prefix != DefaultS3Prefix {
		t.Errorf("prefix default not applied: %q", got.Storage.S3.Prefix)
	}
	if got.Storage.S3.InlineMaxMB != DefaultS3InlineMaxMB {
		t.Errorf("inline_max_mb default not applied: %d", got.Storage.S3.InlineMaxMB)
	}
}
