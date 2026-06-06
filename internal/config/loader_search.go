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

// Package config configuration file location helpers.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// FindConfigFile searches for a configuration file in common locations.
// Returns the path to the first file found, or empty string if none found.
func FindConfigFile() string {
	// Check environment variable first
	if envPath := os.Getenv(EnvConfigFile); envPath != "" {
		cleaned := filepath.Clean(envPath)
		if _, err := os.Stat(cleaned); err == nil { // #nosec G304 -- operator-controlled config path
			return cleaned
		}
	}

	// Search paths in order of priority
	searchPaths := []string{
		DefaultConfigFileName,                                  // Current directory
		filepath.Join(".", DefaultConfigFileName),              // Explicit current directory
		filepath.Join("/etc/apt-proxy", DefaultConfigFileName), // System config
	}

	// Add home directory config if HOME is set
	if home := os.Getenv("HOME"); home != "" {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".config", "apt-proxy", DefaultConfigFileName),
			filepath.Join(home, ".apt-proxy.yaml"),
		)
	}

	for _, path := range searchPaths {
		cleaned := filepath.Clean(path)
		if _, err := os.Stat(cleaned); err == nil { // #nosec G304 -- well-known config search paths
			return cleaned
		}
	}

	return ""
}

// GetConfigFilePaths returns a slice of paths searched for configuration files.
// Useful for debugging and logging.
func GetConfigFilePaths() []string {
	paths := []string{
		DefaultConfigFileName,
		filepath.Join("/etc/apt-proxy", DefaultConfigFileName),
	}

	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths,
			filepath.Join(home, ".config", "apt-proxy", DefaultConfigFileName),
			filepath.Join(home, ".apt-proxy.yaml"),
		)
	}

	return paths
}

// IsConfigFileProvided checks if a config file path was explicitly provided
// via CLI flag or environment variable.
func IsConfigFileProvided() bool {
	// Check environment variable
	if os.Getenv(EnvConfigFile) != "" {
		return true
	}

	// Check CLI args for -config or --config
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-config") || strings.HasPrefix(arg, "--config") {
			return true
		}
	}

	return false
}
