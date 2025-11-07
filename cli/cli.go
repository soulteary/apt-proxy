package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/soulteary/apt-proxy/define"
	"github.com/soulteary/apt-proxy/state"
)

// defaults holds all default configuration values
type defaults struct {
	Host              string
	Port              string
	CacheDir          string
	UbuntuMirror      string
	UbuntuPortsMirror string
	DebianMirror      string
	CentOSMirror      string
	AlpineMirror      string
	ModeName          string
	Debug             bool
}

var (
	// Version is set during build time
	Version string

	// defaultConfig holds default configuration values
	defaultConfig = defaults{
		Host:              "0.0.0.0",
		Port:              "3142",
		CacheDir:          "./.aptcache",
		UbuntuMirror:      "", // "https://mirrors.tuna.tsinghua.edu.cn/ubuntu/"
		UbuntuPortsMirror: "", // "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/"
		DebianMirror:      "", // "https://mirrors.tuna.tsinghua.edu.cn/debian/"
		CentOSMirror:      "", // "https://mirrors.tuna.tsinghua.edu.cn/centos/"
		AlpineMirror:      "", // "https://mirrors.tuna.tsinghua.edu.cn/alpine/"
		ModeName:          define.LINUX_ALL_DISTROS,
		Debug:             false,
	}

	// validModes maps mode strings to their corresponding integer values
	validModes = map[string]int{
		define.LINUX_DISTROS_UBUNTU:       define.TYPE_LINUX_DISTROS_UBUNTU,
		define.LINUX_DISTROS_UBUNTU_PORTS: define.TYPE_LINUX_DISTROS_UBUNTU_PORTS,
		define.LINUX_DISTROS_DEBIAN:       define.TYPE_LINUX_DISTROS_DEBIAN,
		define.LINUX_DISTROS_CENTOS:       define.TYPE_LINUX_DISTROS_CENTOS,
		define.LINUX_DISTROS_ALPINE:       define.TYPE_LINUX_DISTROS_ALPINE,
		define.LINUX_ALL_DISTROS:          define.TYPE_LINUX_ALL_DISTROS,
	}
)

// getProxyMode converts a mode string (e.g., "ubuntu", "debian", "all") to its
// corresponding integer constant. Returns an error if the mode is invalid.
func getProxyMode(mode string) (int, error) {
	if modeValue, exists := validModes[mode]; exists {
		return modeValue, nil
	}
	return 0, fmt.Errorf("invalid mode: %s", mode)
}

// ParseFlags parses command-line flags and returns a Config struct with all
// application settings. It validates the mode parameter and sets up global state.
// Returns an error if flag parsing fails or if an invalid mode is specified.
func ParseFlags() (*Config, error) {
	flags := flag.NewFlagSet("apt-proxy", flag.ContinueOnError)

	var (
		host     string
		port     string
		userMode string
		config   Config
	)

	// Define flags
	flags.StringVar(&host, "host", defaultConfig.Host, "the host to bind to")
	flags.StringVar(&port, "port", defaultConfig.Port, "the port to bind to")
	flags.StringVar(&userMode, "mode", defaultConfig.ModeName,
		"select the mode of system to cache: all / ubuntu / ubuntu-ports / debian / centos / alpine")
	flags.BoolVar(&config.Debug, "debug", defaultConfig.Debug, "whether to output debugging logging")
	flags.StringVar(&config.CacheDir, "cachedir", defaultConfig.CacheDir, "the dir to store cache data in")
	flags.StringVar(&config.Mirrors.Ubuntu, "ubuntu", defaultConfig.UbuntuMirror, "the ubuntu mirror for fetching packages")
	flags.StringVar(&config.Mirrors.UbuntuPorts, "ubuntu-ports", defaultConfig.UbuntuPortsMirror, "the ubuntu ports mirror for fetching packages")
	flags.StringVar(&config.Mirrors.Debian, "debian", defaultConfig.DebianMirror, "the debian mirror for fetching packages")
	flags.StringVar(&config.Mirrors.CentOS, "centos", defaultConfig.CentOSMirror, "the centos mirror for fetching packages")
	flags.StringVar(&config.Mirrors.Alpine, "alpine", defaultConfig.AlpineMirror, "the alpine mirror for fetching packages")

	if err := flags.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	// Validate and set mode
	mode, err := getProxyMode(userMode)
	if err != nil {
		return nil, err
	}
	config.Mode = mode

	// Set listen address
	config.Listen = fmt.Sprintf("%s:%s", host, port)
	config.Version = Version

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
