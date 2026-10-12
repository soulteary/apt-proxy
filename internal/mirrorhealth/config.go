// Package mirrorhealth implements opt-in, snapshot-checked APT mirror failover.
package mirrorhealth

import (
	"fmt"
	"net/url"
	"strings"
)

// Config is YAML-only; zero limits select finite defaults.
type Config struct {
	Enabled               bool         `yaml:"enabled"`
	FailureThreshold      int          `yaml:"failure_threshold"`
	CooldownSeconds       int          `yaml:"cooldown_seconds"`
	ProbeTimeoutSeconds   int          `yaml:"probe_timeout_seconds"`
	MaxManifestBytes      int64        `yaml:"max_manifest_bytes"`
	SnapshotMaxAgeSeconds int          `yaml:"snapshot_max_age_seconds"`
	Repositories          []Repository `yaml:"repositories"`
}

// Repository explicitly maps equivalent archive roots in selection order.
type Repository struct {
	ID              string   `yaml:"id"`
	Mirrors         []string `yaml:"mirrors"`
	PublicQueryKeys []string `yaml:"public_query_keys"`
}

func (cfg Config) defaults() Config {
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = 2
	}
	if cfg.CooldownSeconds == 0 {
		cfg.CooldownSeconds = 30
	}
	if cfg.ProbeTimeoutSeconds == 0 {
		cfg.ProbeTimeoutSeconds = 5
	}
	if cfg.MaxManifestBytes == 0 {
		cfg.MaxManifestBytes = 2 << 20
	}
	if cfg.SnapshotMaxAgeSeconds == 0 {
		cfg.SnapshotMaxAgeSeconds = 3600
	}
	return cfg
}

// Validate rejects ambiguous mappings, credentials and unbounded settings.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return nil
	}
	n := cfg.defaults()
	if n.FailureThreshold < 1 || n.FailureThreshold > 20 || n.CooldownSeconds < 1 || n.CooldownSeconds > 3600 || n.ProbeTimeoutSeconds < 1 || n.ProbeTimeoutSeconds > 30 || n.MaxManifestBytes < 1 || n.MaxManifestBytes > 8<<20 || n.SnapshotMaxAgeSeconds < 1 || n.SnapshotMaxAgeSeconds > 86400 {
		return fmt.Errorf("mirror_health: limits outside supported bounds")
	}
	if len(cfg.Repositories) > 32 || (cfg.Enabled && len(cfg.Repositories) == 0) {
		return fmt.Errorf("mirror_health: configure 1..32 repositories when enabled")
	}
	ids := map[string]bool{}
	for _, repo := range cfg.Repositories {
		if strings.TrimSpace(repo.ID) == "" || ids[repo.ID] {
			return fmt.Errorf("mirror_health: repository IDs must be nonempty and unique")
		}
		ids[repo.ID] = true
		if len(repo.Mirrors) < 2 || len(repo.Mirrors) > 8 {
			return fmt.Errorf("mirror_health: repository %s requires 2..8 mirrors", repo.ID)
		}
		var bases []*url.URL
		for _, raw := range repo.Mirrors {
			u, err := parseBase(raw)
			if err != nil {
				return fmt.Errorf("mirror_health: repository %s has an invalid mirror base: %w", repo.ID, err)
			}
			for _, prev := range bases {
				if sameOrigin(u, prev) && (strings.HasPrefix(u.EscapedPath(), prev.EscapedPath()) || strings.HasPrefix(prev.EscapedPath(), u.EscapedPath())) {
					return fmt.Errorf("mirror_health: repository %s has duplicate or overlapping mirror bases", repo.ID)
				}
			}
			bases = append(bases, u)
		}
		if len(repo.PublicQueryKeys) > 32 {
			return fmt.Errorf("mirror_health: too many public query keys")
		}
		for _, key := range repo.PublicQueryKeys {
			if key == "" || sensitiveKey(key) {
				return fmt.Errorf("mirror_health: public query keys cannot name credentials")
			}
		}
	}
	return nil
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || !strings.HasSuffix(u.Path, "/") {
		return nil, fmt.Errorf("use an absolute HTTP(S) URL ending in /, without credentials, query or fragment")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, fmt.Errorf("mirror base cannot contain dot segments")
		}
	}
	return u, nil
}

func sameOrigin(a, b *url.URL) bool { return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host) }

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range []string{"auth", "token", "pass", "pwd", "secret", "signature", "key", "session", "credential", "user", "login", "jwt"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
