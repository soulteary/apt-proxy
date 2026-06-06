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

// Package distro provides distribution registry for dynamic distribution management.
//
// The Registry is a per-Server value type. Construct one with NewRegistry
// followed by RegisterBuiltins (or LoadFromConfig / Reload for runtime
// configuration). There is no package-level singleton; multiple Servers
// in the same process can hold independent registries.
package distro

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
)

// Registry manages distribution registrations for a single Server.
type Registry struct {
	mu            sync.RWMutex
	distributions map[string]*RegisteredDistribution
	types         map[int]string // type -> id mapping
}

// RegisteredDistribution represents a registered distribution with its configuration.
type RegisteredDistribution struct {
	ID           string
	Name         string
	Type         int
	URLPattern   *regexp.Regexp
	BenchmarkURL string
	GeoMirrorAPI string
	CacheRules   []Rule
	Mirrors      []URLWithAlias
	Aliases      map[string]string
}

// NewRegistry creates an empty registry. Call RegisterBuiltins to seed
// the built-in distributions, or use NewBuiltinRegistry as a one-step
// constructor.
func NewRegistry() *Registry {
	return &Registry{
		distributions: make(map[string]*RegisteredDistribution),
		types:         make(map[int]string),
	}
}

// NewBuiltinRegistry returns a registry pre-populated with the
// compile-time built-in distributions.
func NewBuiltinRegistry() *Registry {
	r := NewRegistry()
	RegisterBuiltins(r)
	return r
}

// Register registers a distribution in the registry.
func (r *Registry) Register(dist *RegisteredDistribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if dist.ID == "" {
		return fmt.Errorf("distribution ID is required")
	}

	if dist.Type == 0 && dist.ID != "all" {
		return fmt.Errorf("distribution type must be non-zero for %s", dist.ID)
	}

	if existingID, exists := r.types[dist.Type]; exists && existingID != dist.ID {
		return fmt.Errorf("type %d already registered for distribution %s", dist.Type, existingID)
	}

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

// GetByID returns a distribution by its ID.
func (r *Registry) GetByID(id string) (*RegisteredDistribution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dist, exists := r.distributions[id]
	return dist, exists
}

// GetByType returns a distribution by its type.
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

// GetAll returns all registered distributions.
//
// The returned map and each value are independent of the registry's internal
// state: the map is fresh, the *RegisteredDistribution structs are shallow
// copies, and the Mirrors / CacheRules / Aliases collections are duplicated
// at the top level. Callers may safely append to / mutate the headers of
// those collections without affecting concurrent registry reads.
//
// Element-level data (URLWithAlias, Rule, *regexp.Regexp) is shared by
// reference; we treat those as immutable once registered, which is true for
// every code path today (registration always builds fresh values).
func (r *Registry) GetAll() map[string]*RegisteredDistribution {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*RegisteredDistribution, len(r.distributions))
	for k, v := range r.distributions {
		distCopy := *v
		if v.Mirrors != nil {
			distCopy.Mirrors = append([]URLWithAlias(nil), v.Mirrors...)
		}
		if v.CacheRules != nil {
			distCopy.CacheRules = append([]Rule(nil), v.CacheRules...)
		}
		if v.Aliases != nil {
			distCopy.Aliases = make(map[string]string, len(v.Aliases))
			for ak, av := range v.Aliases {
				distCopy.Aliases[ak] = av
			}
		}
		result[k] = &distCopy
	}
	return result
}

// Unregister removes a distribution from the registry.
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

// Clear removes all distributions from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.distributions = make(map[string]*RegisteredDistribution)
	r.types = make(map[int]string)
}

// RegisterBuiltins seeds reg with the compile-time built-in distributions.
//
// Built-in entries are guaranteed to satisfy Register's invariants
// (non-empty ID, non-zero Type, no conflicts on a freshly-cleared registry),
// so any error here would indicate a programmer mistake. We surface it via
// panic during initialization rather than silently dropping registrations.
func RegisterBuiltins(reg *Registry) {
	builtins := []*RegisteredDistribution{
		{
			ID:           DistroUbuntu,
			Name:         "Ubuntu",
			Type:         TypeUbuntu,
			URLPattern:   UbuntuHostPattern,
			BenchmarkURL: UbuntuBenchmarkURL,
			GeoMirrorAPI: UbuntuGeoMirrorAPI,
			CacheRules:   UbuntuDefaultCacheRules,
			Mirrors:      BuiltinUbuntuMirrors,
		},
		{
			ID:           DistroUbuntuPorts,
			Name:         "Ubuntu Ports",
			Type:         TypeUbuntuPorts,
			URLPattern:   UbuntuPortsHostPattern,
			BenchmarkURL: UbuntuPortsBenchmarkURL,
			GeoMirrorAPI: UbuntuPortsGeoMirrorAPI,
			CacheRules:   UbuntuPortsDefaultCacheRules,
			Mirrors:      BuiltinUbuntuPortsMirrors,
		},
		{
			ID:           DistroDebian,
			Name:         "Debian",
			Type:         TypeDebian,
			URLPattern:   DebianHostPattern,
			BenchmarkURL: DebianBenchmarkURL,
			CacheRules:   DebianDefaultCacheRules,
			Mirrors:      BuiltinDebianMirrors,
		},
		{
			ID:           DistroCentOS,
			Name:         "CentOS",
			Type:         TypeCentOS,
			URLPattern:   CentosHostPattern,
			BenchmarkURL: CentosBenchmarkURL,
			CacheRules:   CentosDefaultCacheRules,
			Mirrors:      BuiltinCentosMirrors,
		},
		{
			ID:           DistroAlpine,
			Name:         "Alpine Linux",
			Type:         TypeAlpine,
			URLPattern:   AlpineHostPattern,
			BenchmarkURL: AlpineBenchmarkURL,
			CacheRules:   AlpineDefaultCacheRules,
			Mirrors:      BuiltinAlpineMirrors,
		},
	}
	for _, d := range builtins {
		if err := reg.Register(d); err != nil {
			panic(fmt.Sprintf("distro: failed to register built-in %q: %v", d.ID, err))
		}
	}
}

// LoadFromConfig loads a single DistributionConfig and registers it on r.
func (r *Registry) LoadFromConfig(config *DistributionConfig) error {
	urlPattern, err := regexp.Compile(config.URLPattern)
	if err != nil {
		return fmt.Errorf("failed to compile URL pattern: %w", err)
	}

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

	mirrors := make([]URLWithAlias, 0)
	for _, url := range config.Mirrors.Official {
		mirrors = append(mirrors, GenerateBuiltinMirrorItem(url, true))
	}
	for _, url := range config.Mirrors.Custom {
		mirrors = append(mirrors, GenerateBuiltinMirrorItem(url, false))
	}

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

// HostPatternMap returns a map from URL pattern regex to cache rules
// for all distributions in r. Used by the proxy to match incoming
// requests and apply the matching cache rules.
func (r *Registry) HostPatternMap() map[*regexp.Regexp][]Rule {
	all := r.GetAll()
	m := make(map[*regexp.Regexp][]Rule, len(all))
	for _, d := range all {
		if d.URLPattern != nil && len(d.CacheRules) > 0 {
			m[d.URLPattern] = d.CacheRules
		}
	}
	return m
}

// Reload resets r to the built-in distributions, then loads any
// distributions from configPath. When configPath is empty, the default
// search paths inside Loader.Load are used.
//
// Errors loading or registering individual distributions are returned (joined)
// so callers can surface them to operators. The registry is only mutated after
// a successful YAML load: a parse/regex error preserves the previous registry
// state. Per-entry Register failures are accumulated but do not roll back
// previously-applied entries (which would require a deep clone of the
// registry); built-in entries are always reapplied first.
//
// Safe to call at startup and on SIGHUP/API reload.
func (r *Registry) Reload(configPath string) error {
	loader := NewLoader(configPath)
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("loading distributions config: %w", err)
	}

	r.Clear()
	RegisterBuiltins(r)

	if cfg == nil {
		return nil
	}

	var errs []error
	for i := range cfg.Distributions {
		if loadErr := r.LoadFromConfig(&cfg.Distributions[i]); loadErr != nil {
			errs = append(errs, fmt.Errorf("registering %s: %w",
				cfg.Distributions[i].ID, loadErr))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
