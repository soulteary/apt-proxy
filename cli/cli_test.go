package cli

import (
	"os"
	"testing"

	"github.com/soulteary/apt-proxy/distro"
	"github.com/soulteary/cli-kit/testutil"
)

func TestModeToInt(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected int
	}{
		{"ubuntu", distro.LINUX_DISTROS_UBUNTU, distro.TYPE_LINUX_DISTROS_UBUNTU},
		{"ubuntu-ports", distro.LINUX_DISTROS_UBUNTU_PORTS, distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS},
		{"debian", distro.LINUX_DISTROS_DEBIAN, distro.TYPE_LINUX_DISTROS_DEBIAN},
		{"centos", distro.LINUX_DISTROS_CENTOS, distro.TYPE_LINUX_DISTROS_CENTOS},
		{"alpine", distro.LINUX_DISTROS_ALPINE, distro.TYPE_LINUX_DISTROS_ALPINE},
		{"all", distro.LINUX_ALL_DISTROS, distro.TYPE_LINUX_ALL_DISTROS},
		{"unknown defaults to all", "unknown", distro.TYPE_LINUX_ALL_DISTROS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := modeToInt(tt.mode)
			if result != tt.expected {
				t.Errorf("modeToInt(%q) = %d, want %d", tt.mode, result, tt.expected)
			}
		})
	}
}

func TestParseFlagsWithEnvVars(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Set environment variables
	envMgr.Set(EnvHost, "127.0.0.1")
	envMgr.Set(EnvPort, "8080")
	envMgr.Set(EnvMode, "debian")
	envMgr.Set(EnvCacheDir, "/tmp/aptcache")
	envMgr.Set(EnvDebug, "true")
	envMgr.Set(EnvUbuntu, "https://mirrors.example.com/ubuntu/")

	// Set minimal args (program name only)
	os.Args = []string{"apt-proxy"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	// Verify environment variables are used
	if config.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q, want %q", config.Listen, "127.0.0.1:8080")
	}
	if config.Mode != distro.TYPE_LINUX_DISTROS_DEBIAN {
		t.Errorf("Mode = %d, want %d", config.Mode, distro.TYPE_LINUX_DISTROS_DEBIAN)
	}
	if config.CacheDir != "/tmp/aptcache" {
		t.Errorf("CacheDir = %q, want %q", config.CacheDir, "/tmp/aptcache")
	}
	if !config.Debug {
		t.Error("Debug = false, want true")
	}
	if config.Mirrors.Ubuntu != "https://mirrors.example.com/ubuntu/" {
		t.Errorf("Mirrors.Ubuntu = %q, want %q", config.Mirrors.Ubuntu, "https://mirrors.example.com/ubuntu/")
	}
}

func TestParseFlagsCLIPriority(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Set environment variables (lower priority)
	envMgr.Set(EnvHost, "127.0.0.1")
	envMgr.Set(EnvPort, "8080")
	envMgr.Set(EnvMode, "debian")

	// Set CLI args (higher priority)
	os.Args = []string{"apt-proxy", "-host", "192.168.1.1", "-port", "9090", "-mode", "ubuntu"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	// CLI flags should take priority over environment variables
	if config.Listen != "192.168.1.1:9090" {
		t.Errorf("Listen = %q, want %q (CLI should override ENV)", config.Listen, "192.168.1.1:9090")
	}
	if config.Mode != distro.TYPE_LINUX_DISTROS_UBUNTU {
		t.Errorf("Mode = %d, want %d (CLI should override ENV)", config.Mode, distro.TYPE_LINUX_DISTROS_UBUNTU)
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Ensure no env vars are set that would override defaults
	os.Unsetenv(EnvHost)
	os.Unsetenv(EnvPort)
	os.Unsetenv(EnvMode)
	os.Unsetenv(EnvCacheDir)
	os.Unsetenv(EnvDebug)

	// Set minimal args
	os.Args = []string{"apt-proxy"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	// Verify defaults are used
	if config.Listen != "0.0.0.0:3142" {
		t.Errorf("Listen = %q, want %q (default)", config.Listen, "0.0.0.0:3142")
	}
	if config.Mode != distro.TYPE_LINUX_ALL_DISTROS {
		t.Errorf("Mode = %d, want %d (default)", config.Mode, distro.TYPE_LINUX_ALL_DISTROS)
	}
	if config.CacheDir != DefaultCacheDir {
		t.Errorf("CacheDir = %q, want %q (default)", config.CacheDir, DefaultCacheDir)
	}
	if config.Debug {
		t.Error("Debug = true, want false (default)")
	}
}

func TestParseFlagsInvalidModeFallback(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Set invalid mode via CLI
	// Note: cli-kit's ResolveEnum uses graceful degradation - when CLI value is invalid,
	// it falls back to ENV, then to default value. This is the expected behavior for
	// robust configuration handling.
	os.Args = []string{"apt-proxy", "-mode", "invalid-mode"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v (graceful degradation should not error)", err)
	}

	// Invalid mode should fall back to default (all)
	if config.Mode != distro.TYPE_LINUX_ALL_DISTROS {
		t.Errorf("Mode = %d, want %d (should fallback to default 'all')", config.Mode, distro.TYPE_LINUX_ALL_DISTROS)
	}
}

func TestParseFlagsInvalidModeEnvFallback(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Set valid mode via ENV and invalid mode via CLI
	envMgr.Set(EnvMode, "debian")
	os.Args = []string{"apt-proxy", "-mode", "invalid-mode"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	// Invalid CLI mode should fall back to valid ENV mode
	if config.Mode != distro.TYPE_LINUX_DISTROS_DEBIAN {
		t.Errorf("Mode = %d, want %d (should fallback to ENV 'debian')", config.Mode, distro.TYPE_LINUX_DISTROS_DEBIAN)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty cache dir",
			config: &Config{
				CacheDir: "",
				Listen:   "0.0.0.0:3142",
			},
			wantErr: true,
		},
		{
			name: "empty listen address",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateConfigTLS(t *testing.T) {
	// Create temporary certificate and key files for testing
	tempDir := t.TempDir()
	certFile := tempDir + "/cert.pem"
	keyFile := tempDir + "/key.pem"

	// Create dummy files
	if err := os.WriteFile(certFile, []byte("dummy cert"), 0644); err != nil {
		t.Fatalf("Failed to create temp cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("dummy key"), 0644); err != nil {
		t.Fatalf("Failed to create temp key file: %v", err)
	}

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "TLS enabled without cert file",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: "",
					KeyFile:  keyFile,
				},
			},
			wantErr: true,
			errMsg:  "TLS certificate file must be specified",
		},
		{
			name: "TLS enabled without key file",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: certFile,
					KeyFile:  "",
				},
			},
			wantErr: true,
			errMsg:  "TLS key file must be specified",
		},
		{
			name: "TLS enabled with non-existent cert file",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: "/non/existent/cert.pem",
					KeyFile:  keyFile,
				},
			},
			wantErr: true,
			errMsg:  "TLS certificate file not found",
		},
		{
			name: "TLS enabled with non-existent key file",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: certFile,
					KeyFile:  "/non/existent/key.pem",
				},
			},
			wantErr: true,
			errMsg:  "TLS key file not found",
		},
		{
			name: "TLS enabled with valid files",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: certFile,
					KeyFile:  keyFile,
				},
			},
			wantErr: false,
		},
		{
			name: "TLS disabled (no validation needed)",
			config: &Config{
				CacheDir: "/tmp/cache",
				Listen:   "0.0.0.0:3142",
				TLS: TLSConfig{
					Enabled: false,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestParseFlagsTLS(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Test TLS flags via CLI
	os.Args = []string{"apt-proxy", "-tls", "-tls-cert", "/path/to/cert.pem", "-tls-key", "/path/to/key.pem"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	if !config.TLS.Enabled {
		t.Error("TLS.Enabled = false, want true")
	}
	if config.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("TLS.CertFile = %q, want %q", config.TLS.CertFile, "/path/to/cert.pem")
	}
	if config.TLS.KeyFile != "/path/to/key.pem" {
		t.Errorf("TLS.KeyFile = %q, want %q", config.TLS.KeyFile, "/path/to/key.pem")
	}
}

func TestParseFlagsTLSEnvVars(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Set TLS environment variables
	envMgr.Set(EnvTLSEnabled, "true")
	envMgr.Set(EnvTLSCertFile, "/env/path/cert.pem")
	envMgr.Set(EnvTLSKeyFile, "/env/path/key.pem")

	// Set minimal args
	os.Args = []string{"apt-proxy"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	if !config.TLS.Enabled {
		t.Error("TLS.Enabled = false, want true (from ENV)")
	}
	if config.TLS.CertFile != "/env/path/cert.pem" {
		t.Errorf("TLS.CertFile = %q, want %q", config.TLS.CertFile, "/env/path/cert.pem")
	}
	if config.TLS.KeyFile != "/env/path/key.pem" {
		t.Errorf("TLS.KeyFile = %q, want %q", config.TLS.KeyFile, "/env/path/key.pem")
	}
}

func TestParseFlagsTLSDefaults(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Use testutil to manage environment variables
	envMgr := testutil.NewEnvManager()
	defer envMgr.Cleanup()

	// Ensure no TLS env vars are set
	os.Unsetenv(EnvTLSEnabled)
	os.Unsetenv(EnvTLSCertFile)
	os.Unsetenv(EnvTLSKeyFile)

	// Set minimal args
	os.Args = []string{"apt-proxy"}

	config, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	// Verify TLS defaults
	if config.TLS.Enabled {
		t.Error("TLS.Enabled = true, want false (default)")
	}
	if config.TLS.CertFile != "" {
		t.Errorf("TLS.CertFile = %q, want empty string (default)", config.TLS.CertFile)
	}
	if config.TLS.KeyFile != "" {
		t.Errorf("TLS.KeyFile = %q, want empty string (default)", config.TLS.KeyFile)
	}
}

// contains checks if substr is contained in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAllowedModes(t *testing.T) {
	// Verify allowedModes contains all expected values
	expectedModes := []string{
		distro.LINUX_ALL_DISTROS,
		distro.LINUX_DISTROS_UBUNTU,
		distro.LINUX_DISTROS_UBUNTU_PORTS,
		distro.LINUX_DISTROS_DEBIAN,
		distro.LINUX_DISTROS_CENTOS,
		distro.LINUX_DISTROS_ALPINE,
	}

	if len(allowedModes) != len(expectedModes) {
		t.Errorf("allowedModes has %d items, want %d", len(allowedModes), len(expectedModes))
	}

	for _, expected := range expectedModes {
		found := false
		for _, mode := range allowedModes {
			if mode == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("allowedModes missing %q", expected)
		}
	}
}
