// Package distro provides distribution registry for dynamic distribution management
package distro

import (
	"fmt"
	"regexp"
	"sync"
)

// Registry manages distribution registrations
type Registry struct {
	mu            sync.RWMutex
	distributions map[string]*RegisteredDistribution
	types         map[int]string // type -> id mapping
}

// RegisteredDistribution represents a registered distribution with its configuration
type RegisteredDistribution struct {
	ID           string
	Name         string
	Type         int
	URLPattern   *regexp.Regexp
	BenchmarkURL string
	GeoMirrorAPI string
	CacheRules   []Rule
	Mirrors      []UrlWithAlias
	Aliases      map[string]string
}

var (
	globalRegistry *Registry
	registryOnce   sync.Once
)

// GetRegistry returns the global distribution registry
func GetRegistry() *Registry {
	registryOnce.Do(func() {
		globalRegistry = NewRegistry()
		// Register built-in distributions
		registerBuiltinDistributions(globalRegistry)
	})
	return globalRegistry
}

// NewRegistry creates a new distribution registry
func NewRegistry() *Registry {
	return &Registry{
		distributions: make(map[string]*RegisteredDistribution),
		types:         make(map[int]string),
	}
}

// Register registers a distribution in the registry
func (r *Registry) Register(dist *RegisteredDistribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if dist.ID == "" {
		return fmt.Errorf("distribution ID is required")
	}

	if dist.Type == 0 && dist.ID != "all" {
		return fmt.Errorf("distribution type must be non-zero for %s", dist.ID)
	}

	// Check for type conflicts
	if existingID, exists := r.types[dist.Type]; exists && existingID != dist.ID {
		return fmt.Errorf("type %d already registered for distribution %s", dist.Type, existingID)
	}

	// Check for ID conflicts (allow overwrite with same type)
	if existing, exists := r.distributions[dist.ID]; exists {
		if existing.Type != dist.Type {
			return fmt.Errorf("distribution %s already registered with different type", dist.ID)
		}
	}

	r.distributions[dist.ID] = dist
	if dist.Type != 0 {
		r.types[dist.Type] = dist.ID
	}

	return nil
}

// GetByID returns a distribution by its ID
func (r *Registry) GetByID(id string) (*RegisteredDistribution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dist, exists := r.distributions[id]
	return dist, exists
}

// GetByType returns a distribution by its type
func (r *Registry) GetByType(distType int) (*RegisteredDistribution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, exists := r.types[distType]
	if !exists {
		return nil, false
	}

	dist, exists := r.distributions[id]
	return dist, exists
}

// GetAll returns all registered distributions
func (r *Registry) GetAll() map[string]*RegisteredDistribution {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*RegisteredDistribution)
	for k, v := range r.distributions {
		// Create a copy to avoid race conditions
		distCopy := *v
		result[k] = &distCopy
	}
	return result
}

// Unregister removes a distribution from the registry
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	dist, exists := r.distributions[id]
	if !exists {
		return fmt.Errorf("distribution %s not found", id)
	}

	delete(r.distributions, id)
	if dist.Type != 0 {
		delete(r.types, dist.Type)
	}

	return nil
}

// Clear removes all distributions from the registry
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.distributions = make(map[string]*RegisteredDistribution)
	r.types = make(map[int]string)
}

// registerBuiltinDistributions registers the built-in distributions
func registerBuiltinDistributions(reg *Registry) {
	// Register Ubuntu
	reg.Register(&RegisteredDistribution{
		ID:           LINUX_DISTROS_UBUNTU,
		Name:         "Ubuntu",
		Type:         TYPE_LINUX_DISTROS_UBUNTU,
		URLPattern:   UBUNTU_HOST_PATTERN,
		BenchmarkURL: UBUNTU_BENCHMARK_URL,
		GeoMirrorAPI: UBUNTU_GEO_MIRROR_API,
		CacheRules:   UBUNTU_DEFAULT_CACHE_RULES,
		Mirrors:      BUILDIN_UBUNTU_MIRRORS,
	})

	// Register Ubuntu Ports
	reg.Register(&RegisteredDistribution{
		ID:           LINUX_DISTROS_UBUNTU_PORTS,
		Name:         "Ubuntu Ports",
		Type:         TYPE_LINUX_DISTROS_UBUNTU_PORTS,
		URLPattern:   UBUNTU_PORTS_HOST_PATTERN,
		BenchmarkURL: UBUNTU_PORTS_BENCHMARK_URL,
		GeoMirrorAPI: UBUNTU_PORTS_GEO_MIRROR_API,
		CacheRules:   UBUNTU_PORTS_DEFAULT_CACHE_RULES,
		Mirrors:      BUILDIN_UBUNTU_PORTS_MIRRORS,
	})

	// Register Debian
	reg.Register(&RegisteredDistribution{
		ID:           LINUX_DISTROS_DEBIAN,
		Name:         "Debian",
		Type:         TYPE_LINUX_DISTROS_DEBIAN,
		URLPattern:   DEBIAN_HOST_PATTERN,
		BenchmarkURL: DEBIAN_BENCHMARK_URL,
		CacheRules:   DEBIAN_DEFAULT_CACHE_RULES,
		Mirrors:      BUILDIN_DEBIAN_MIRRORS,
	})

	// Register CentOS
	reg.Register(&RegisteredDistribution{
		ID:           LINUX_DISTROS_CENTOS,
		Name:         "CentOS",
		Type:         TYPE_LINUX_DISTROS_CENTOS,
		URLPattern:   CENTOS_HOST_PATTERN,
		BenchmarkURL: CENTOS_BENCHMARK_URL,
		CacheRules:   CENTOS_DEFAULT_CACHE_RULES,
		Mirrors:      BUILDIN_CENTOS_MIRRORS,
	})

	// Register Alpine
	reg.Register(&RegisteredDistribution{
		ID:           LINUX_DISTROS_ALPINE,
		Name:         "Alpine Linux",
		Type:         TYPE_LINUX_DISTROS_ALPINE,
		URLPattern:   ALPINE_HOST_PATTERN,
		BenchmarkURL: ALPINE_BENCHMARK_URL,
		CacheRules:   ALPINE_DEFAULT_CACHE_RULES,
		Mirrors:      BUILDIN_ALPINE_MIRRORS,
	})
}

// LoadFromConfig loads distributions from a DistributionConfig and registers them
func (r *Registry) LoadFromConfig(config *DistributionConfig) error {
	// Compile URL pattern
	urlPattern, err := regexp.Compile(config.URLPattern)
	if err != nil {
		return fmt.Errorf("failed to compile URL pattern: %w", err)
	}

	// Convert cache rules
	cacheRules := make([]Rule, 0, len(config.CacheRules))
	for _, ruleConfig := range config.CacheRules {
		pattern, err := regexp.Compile(ruleConfig.Pattern)
		if err != nil {
			return fmt.Errorf("failed to compile cache rule pattern %s: %w", ruleConfig.Pattern, err)
		}

		cacheRules = append(cacheRules, Rule{
			OS:           config.Type,
			Pattern:      pattern,
			CacheControl: ruleConfig.CacheControl,
			Rewrite:      ruleConfig.Rewrite,
		})
	}

	// Convert mirrors
	mirrors := make([]UrlWithAlias, 0)
	for _, url := range config.Mirrors.Official {
		mirror := GenerateBuildInMirorItem(url, true)
		mirrors = append(mirrors, mirror)
	}
	for _, url := range config.Mirrors.Custom {
		mirror := GenerateBuildInMirorItem(url, false)
		mirrors = append(mirrors, mirror)
	}

	// Register the distribution
	dist := &RegisteredDistribution{
		ID:           config.ID,
		Name:         config.Name,
		Type:         config.Type,
		URLPattern:   urlPattern,
		BenchmarkURL: config.BenchmarkURL,
		GeoMirrorAPI: config.GeoMirrorAPI,
		CacheRules:   cacheRules,
		Mirrors:      mirrors,
		Aliases:      config.Aliases,
	}

	return r.Register(dist)
}

// GetHostPatternMap returns a map from URL pattern regex to cache rules for all
// registered distributions. Used by the proxy to match requests and apply rules.
func GetHostPatternMap() map[*regexp.Regexp][]Rule {
	reg := GetRegistry()
	all := reg.GetAll()
	m := make(map[*regexp.Regexp][]Rule, len(all))
	for _, d := range all {
		if d.URLPattern != nil && len(d.CacheRules) > 0 {
			m[d.URLPattern] = d.CacheRules
		}
	}
	return m
}

// ReloadDistributionsConfig resets the global registry to built-in distributions,
// then loads and applies distributions from the given config file path.
// When configPath is empty, Load() still tries default paths (./config/distributions.yaml,
// ./distributions.yaml, /etc/apt-proxy/distributions.yaml, etc.).
// Safe to call at startup and on SIGHUP/API reload.
func ReloadDistributionsConfig(configPath string) {
	reg := GetRegistry()
	reg.Clear()
	registerBuiltinDistributions(reg)
	loader := NewLoader(configPath)
	cfg, err := loader.Load()
	if err != nil || cfg == nil {
		return
	}
	for i := range cfg.Distributions {
		_ = reg.LoadFromConfig(&cfg.Distributions[i])
	}
}
