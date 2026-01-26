package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/soulteary/apt-proxy/define"
	"github.com/soulteary/apt-proxy/internal/mirrors"
	"github.com/soulteary/apt-proxy/state"
	"github.com/soulteary/cli-kit/configutil"
)

// Environment variable names for configuration
const (
	EnvHost        = "APT_PROXY_HOST"
	EnvPort        = "APT_PROXY_PORT"
	EnvMode        = "APT_PROXY_MODE"
	EnvCacheDir    = "APT_PROXY_CACHEDIR"
	EnvDebug       = "APT_PROXY_DEBUG"
	EnvUbuntu      = "APT_PROXY_UBUNTU"
	EnvUbuntuPorts = "APT_PROXY_UBUNTU_PORTS"
	EnvDebian      = "APT_PROXY_DEBIAN"
	EnvCentOS      = "APT_PROXY_CENTOS"
	EnvAlpine      = "APT_PROXY_ALPINE"
)

// Default configuration values
const (
	DefaultHost     = "0.0.0.0"
	DefaultPort     = "3142"
	DefaultCacheDir = "./.aptcache"
)

var (
	// allowedModes defines the valid mode values for proxy operation
	allowedModes = []string{
		define.LINUX_ALL_DISTROS,
		define.LINUX_DISTROS_UBUNTU,
		define.LINUX_DISTROS_UBUNTU_PORTS,
		define.LINUX_DISTROS_DEBIAN,
		define.LINUX_DISTROS_CENTOS,
		define.LINUX_DISTROS_ALPINE,
	}
)

// modeToInt converts a validated mode string to its corresponding integer constant.
// This function should only be called after mode validation via configutil.ResolveEnum.
func modeToInt(mode string) int {
	switch mode {
	case define.LINUX_DISTROS_UBUNTU:
		return define.TYPE_LINUX_DISTROS_UBUNTU
	case define.LINUX_DISTROS_UBUNTU_PORTS:
		return define.TYPE_LINUX_DISTROS_UBUNTU_PORTS
	case define.LINUX_DISTROS_DEBIAN:
		return define.TYPE_LINUX_DISTROS_DEBIAN
	case define.LINUX_DISTROS_CENTOS:
		return define.TYPE_LINUX_DISTROS_CENTOS
	case define.LINUX_DISTROS_ALPINE:
		return define.TYPE_LINUX_DISTROS_ALPINE
	default:
		return define.TYPE_LINUX_ALL_DISTROS
	}
}

// ParseFlags parses command-line flags and returns a Config struct with all
// application settings. It validates the mode parameter and sets up global state.
// Configuration priority: CLI flag > environment variable > default value.
// Returns an error if flag parsing fails or if an invalid mode is specified.
func ParseFlags() (*Config, error) {
	flags := flag.NewFlagSet("apt-proxy", flag.ContinueOnError)

	// Define flags (for CLI compatibility and help text)
	flags.String("host", DefaultHost, "the host to bind to")
	flags.String("port", DefaultPort, "the port to bind to")
	flags.String("mode", define.LINUX_ALL_DISTROS,
		"select the mode of system to cache: all / ubuntu / ubuntu-ports / debian / centos / alpine")
	flags.Bool("debug", false, "whether to output debugging logging")
	flags.String("cachedir", DefaultCacheDir, "the dir to store cache data in")
	flags.String("ubuntu", "", "the ubuntu mirror for fetching packages")
	flags.String("ubuntu-ports", "", "the ubuntu ports mirror for fetching packages")
	flags.String("debian", "", "the debian mirror for fetching packages")
	flags.String("centos", "", "the centos mirror for fetching packages")
	flags.String("alpine", "", "the alpine mirror for fetching packages")

	if err := flags.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	// Resolve configuration with priority: CLI > ENV > default
	host := configutil.ResolveString(flags, "host", EnvHost, DefaultHost, true)
	port := configutil.ResolveString(flags, "port", EnvPort, DefaultPort, true)
	debug := configutil.ResolveBool(flags, "debug", EnvDebug, false)
	cacheDir := configutil.ResolveString(flags, "cachedir", EnvCacheDir, DefaultCacheDir, true)

	// Validate and resolve mode using enum validation
	modeName, err := configutil.ResolveEnum(flags, "mode", EnvMode, define.LINUX_ALL_DISTROS, allowedModes, false)
	if err != nil {
		return nil, fmt.Errorf("invalid mode: %w", err)
	}

	// Resolve mirror configurations
	ubuntu := configutil.ResolveString(flags, "ubuntu", EnvUbuntu, "", true)
	ubuntuPorts := configutil.ResolveString(flags, "ubuntu-ports", EnvUbuntuPorts, "", true)
	debian := configutil.ResolveString(flags, "debian", EnvDebian, "", true)
	centos := configutil.ResolveString(flags, "centos", EnvCentOS, "", true)
	alpine := configutil.ResolveString(flags, "alpine", EnvAlpine, "", true)

	// Build configuration
	config := Config{
		Debug:    debug,
		CacheDir: cacheDir,
		Mode:     modeToInt(modeName),
		Mirrors: MirrorConfig{
			Ubuntu:      ubuntu,
			UbuntuPorts: ubuntuPorts,
			Debian:      debian,
			CentOS:      centos,
			Alpine:      alpine,
		},
	}

	// Set listen address using templates
	listenAddr, err := mirrors.BuildListenAddress(host, port)
	if err != nil {
		// Fallback to fmt.Sprintf if template fails
		config.Listen = fmt.Sprintf("%s:%s", host, port)
	} else {
		config.Listen = listenAddr
	}

	// Update global state
	if err := updateGlobalState(&config); err != nil {
		return nil, fmt.Errorf("updating global state: %w", err)
	}

	return &config, nil
}

// updateGlobalState updates the global state with the current configuration,
// including proxy mode and mirror URLs for all supported distributions.
// This enables components throughout the application to access configuration.
func updateGlobalState(config *Config) error {
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

	if config.CacheDir == "" {
		return fmt.Errorf("cache directory must be specified")
	}

	if config.Listen == "" {
		return fmt.Errorf("listen address must be specified")
	}

	return nil
}
