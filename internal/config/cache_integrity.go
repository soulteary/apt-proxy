package config

import "fmt"

func ValidateCacheIntegrity(mode string) error {
	switch mode {
	case "", "sha256", "none":
		return nil
	default:
		return fmt.Errorf("cache.integrity must be sha256 or none")
	}
}
