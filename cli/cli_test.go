package cli

import (
	"os"
	"testing"

	"github.com/soulteary/apt-proxy/define"
	"github.com/soulteary/cli-kit/testutil"
)

func TestModeToInt(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected int
	}{
		{"ubuntu", define.LINUX_DISTROS_UBUNTU, define.TYPE_LINUX_DISTROS_UBUNTU},
		{"ubuntu-ports", define.LINUX_DISTROS_UBUNTU_PORTS, define.TYPE_LINUX_DISTROS_UBUNTU_PORTS},
		{"debian", define.LINUX_DISTROS_DEBIAN, define.TYPE_LINUX_DISTROS_DEBIAN},
		{"centos", define.LINUX_DISTROS_CENTOS, define.TYPE_LINUX_DISTROS_CENTOS},
		{"alpine", define.LINUX_DISTROS_ALPINE, define.TYPE_LINUX_DISTROS_ALPINE},
		{"all", define.LINUX_ALL_DISTROS, define.TYPE_LINUX_ALL_DISTROS},
		{"unknown defaults to all", "unknown", define.TYPE_LINUX_ALL_DISTROS},
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
	if config.Mode != define.TYPE_LINUX_DISTROS_DEBIAN {
		t.Errorf("Mode = %d, want %d", config.Mode, define.TYPE_LINUX_DISTROS_DEBIAN)
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
	if config.Mode != define.TYPE_LINUX_DISTROS_UBUNTU {
		t.Errorf("Mode = %d, want %d (CLI should override ENV)", config.Mode, define.TYPE_LINUX_DISTROS_UBUNTU)
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
	if config.Mode != define.TYPE_LINUX_ALL_DISTROS {
		t.Errorf("Mode = %d, want %d (default)", config.Mode, define.TYPE_LINUX_ALL_DISTROS)
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
	if config.Mode != define.TYPE_LINUX_ALL_DISTROS {
		t.Errorf("Mode = %d, want %d (should fallback to default 'all')", config.Mode, define.TYPE_LINUX_ALL_DISTROS)
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
	if config.Mode != define.TYPE_LINUX_DISTROS_DEBIAN {
		t.Errorf("Mode = %d, want %d (should fallback to ENV 'debian')", config.Mode, define.TYPE_LINUX_DISTROS_DEBIAN)
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

func TestAllowedModes(t *testing.T) {
	// Verify allowedModes contains all expected values
	expectedModes := []string{
		define.LINUX_ALL_DISTROS,
		define.LINUX_DISTROS_UBUNTU,
		define.LINUX_DISTROS_UBUNTU_PORTS,
		define.LINUX_DISTROS_DEBIAN,
		define.LINUX_DISTROS_CENTOS,
		define.LINUX_DISTROS_ALPINE,
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
