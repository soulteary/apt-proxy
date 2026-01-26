// Package state provides application state management with support for both
// global singleton access (backward compatibility) and dependency injection patterns.
package state

import (
	"net/url"
	"sync"

	"github.com/soulteary/apt-proxy/distro"
	mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

// MirrorState manages mirror URL states for a specific distribution
type MirrorState struct {
	url      *url.URL
	distType int
	mutex    sync.RWMutex
}

// NewMirrorState creates a new MirrorState instance
func NewMirrorState(distType int) *MirrorState {
	return &MirrorState{
		distType: distType,
	}
}

// Set updates the mirror URL
func (m *MirrorState) Set(input string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if input == "" {
		m.url = nil
		return
	}

	mirror := input
	if alias := mirrors.GetMirrorURLByAliases(m.distType, input); alias != "" {
		mirror = alias
	}

	url, err := url.Parse(mirror)
	if err != nil {
		m.url = nil
		return
	}
	m.url = url
}

// Get returns the current mirror URL
func (m *MirrorState) Get() *url.URL {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.url
}

// Reset clears the mirror URL
func (m *MirrorState) Reset() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.url = nil
}

// Clone creates a copy of the MirrorState
func (m *MirrorState) Clone() *MirrorState {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	clone := NewMirrorState(m.distType)
	if m.url != nil {
		// Deep copy the URL
		urlCopy := *m.url
		clone.url = &urlCopy
	}
	return clone
}

// AppState manages the complete application state including proxy mode
// and mirror configurations for all supported distributions.
// This struct supports dependency injection for better testability.
type AppState struct {
	proxyMode   int
	modeMutex   sync.RWMutex
	Ubuntu      *MirrorState
	UbuntuPorts *MirrorState
	Debian      *MirrorState
	CentOS      *MirrorState
	Alpine      *MirrorState
}

// NewAppState creates a new AppState instance with initialized mirror states
func NewAppState() *AppState {
	return &AppState{
		Ubuntu:      NewMirrorState(distro.TYPE_LINUX_DISTROS_UBUNTU),
		UbuntuPorts: NewMirrorState(distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS),
		Debian:      NewMirrorState(distro.TYPE_LINUX_DISTROS_DEBIAN),
		CentOS:      NewMirrorState(distro.TYPE_LINUX_DISTROS_CENTOS),
		Alpine:      NewMirrorState(distro.TYPE_LINUX_DISTROS_ALPINE),
	}
}

// SetProxyMode sets the proxy mode
func (s *AppState) SetProxyMode(mode int) {
	s.modeMutex.Lock()
	defer s.modeMutex.Unlock()
	s.proxyMode = mode
}

// GetProxyMode returns the current proxy mode
func (s *AppState) GetProxyMode() int {
	s.modeMutex.RLock()
	defer s.modeMutex.RUnlock()
	return s.proxyMode
}

// SetMirror sets the mirror URL for a specific distribution
func (s *AppState) SetMirror(distType int, input string) {
	switch distType {
	case distro.TYPE_LINUX_DISTROS_UBUNTU:
		s.Ubuntu.Set(input)
	case distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		s.UbuntuPorts.Set(input)
	case distro.TYPE_LINUX_DISTROS_DEBIAN:
		s.Debian.Set(input)
	case distro.TYPE_LINUX_DISTROS_CENTOS:
		s.CentOS.Set(input)
	case distro.TYPE_LINUX_DISTROS_ALPINE:
		s.Alpine.Set(input)
	}
}

// GetMirror returns the mirror URL for a specific distribution
func (s *AppState) GetMirror(distType int) *url.URL {
	switch distType {
	case distro.TYPE_LINUX_DISTROS_UBUNTU:
		return s.Ubuntu.Get()
	case distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return s.UbuntuPorts.Get()
	case distro.TYPE_LINUX_DISTROS_DEBIAN:
		return s.Debian.Get()
	case distro.TYPE_LINUX_DISTROS_CENTOS:
		return s.CentOS.Get()
	case distro.TYPE_LINUX_DISTROS_ALPINE:
		return s.Alpine.Get()
	default:
		return nil
	}
}

// ResetAll resets all mirror states
func (s *AppState) ResetAll() {
	s.Ubuntu.Reset()
	s.UbuntuPorts.Reset()
	s.Debian.Reset()
	s.CentOS.Reset()
	s.Alpine.Reset()
}

// Clone creates a deep copy of the AppState
func (s *AppState) Clone() *AppState {
	s.modeMutex.RLock()
	mode := s.proxyMode
	s.modeMutex.RUnlock()

	clone := &AppState{
		proxyMode:   mode,
		Ubuntu:      s.Ubuntu.Clone(),
		UbuntuPorts: s.UbuntuPorts.Clone(),
		Debian:      s.Debian.Clone(),
		CentOS:      s.CentOS.Clone(),
		Alpine:      s.Alpine.Clone(),
	}
	return clone
}

// ============================================================================
// Global Singleton - Backward Compatibility Layer
// ============================================================================

var (
	// globalState is the default global state instance
	globalState     *AppState
	globalStateMu   sync.RWMutex
	globalStateOnce sync.Once
)

// initGlobalState initializes the global state singleton
func initGlobalState() {
	globalStateOnce.Do(func() {
		globalState = NewAppState()
	})
}

// Global returns the global AppState instance.
// This is primarily for dependency injection scenarios where you need
// to pass the state explicitly.
func Global() *AppState {
	initGlobalState()
	return globalState
}

// SetGlobal replaces the global AppState instance.
// This is useful for testing or when you want to use a custom state instance.
// Note: This function is not thread-safe during initialization.
func SetGlobal(state *AppState) {
	globalStateMu.Lock()
	defer globalStateMu.Unlock()
	globalState = state
}

// ============================================================================
// Backward Compatibility Functions - Proxy Mode
// ============================================================================

func SetProxyMode(mode int) {
	initGlobalState()
	globalState.SetProxyMode(mode)
}

func GetProxyMode() int {
	initGlobalState()
	return globalState.GetProxyMode()
}

// ============================================================================
// Backward Compatibility Functions - Mirror States (Global Variables Pattern)
// ============================================================================

// These variables maintain backward compatibility with existing code
// that accesses mirrors directly. They delegate to the global AppState.
var (
	// UbuntuMirror provides backward compatible access to Ubuntu mirror state
	UbuntuMirror = &mirrorProxy{distType: distro.TYPE_LINUX_DISTROS_UBUNTU}
	// UbuntuPortsMirror provides backward compatible access to Ubuntu Ports mirror state
	UbuntuPortsMirror = &mirrorProxy{distType: distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS}
	// DebianMirror provides backward compatible access to Debian mirror state
	DebianMirror = &mirrorProxy{distType: distro.TYPE_LINUX_DISTROS_DEBIAN}
	// CentOSMirror provides backward compatible access to CentOS mirror state
	CentOSMirror = &mirrorProxy{distType: distro.TYPE_LINUX_DISTROS_CENTOS}
	// AlpineMirror provides backward compatible access to Alpine mirror state
	AlpineMirror = &mirrorProxy{distType: distro.TYPE_LINUX_DISTROS_ALPINE}
)

// mirrorProxy provides a proxy to the global state's mirror states
type mirrorProxy struct {
	distType int
}

func (p *mirrorProxy) Set(input string) {
	initGlobalState()
	globalState.SetMirror(p.distType, input)
}

func (p *mirrorProxy) Get() *url.URL {
	initGlobalState()
	return globalState.GetMirror(p.distType)
}

func (p *mirrorProxy) Reset() {
	initGlobalState()
	switch p.distType {
	case distro.TYPE_LINUX_DISTROS_UBUNTU:
		globalState.Ubuntu.Reset()
	case distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		globalState.UbuntuPorts.Reset()
	case distro.TYPE_LINUX_DISTROS_DEBIAN:
		globalState.Debian.Reset()
	case distro.TYPE_LINUX_DISTROS_CENTOS:
		globalState.CentOS.Reset()
	case distro.TYPE_LINUX_DISTROS_ALPINE:
		globalState.Alpine.Reset()
	}
}

// ============================================================================
// Backward Compatibility Convenience Functions
// ============================================================================

func SetUbuntuMirror(input string) { UbuntuMirror.Set(input) }
func GetUbuntuMirror() *url.URL    { return UbuntuMirror.Get() }
func ResetUbuntuMirror()           { UbuntuMirror.Reset() }

func SetUbuntuPortsMirror(input string) { UbuntuPortsMirror.Set(input) }
func GetUbuntuPortsMirror() *url.URL    { return UbuntuPortsMirror.Get() }
func ResetUbuntuPortsMirror()           { UbuntuPortsMirror.Reset() }

func SetDebianMirror(input string) { DebianMirror.Set(input) }
func GetDebianMirror() *url.URL    { return DebianMirror.Get() }
func ResetDebianMirror()           { DebianMirror.Reset() }

func SetCentOSMirror(input string) { CentOSMirror.Set(input) }
func GetCentOSMirror() *url.URL    { return CentOSMirror.Get() }
func ResetCentOSMirror()           { CentOSMirror.Reset() }

func SetAlpineMirror(input string) { AlpineMirror.Set(input) }
func GetAlpineMirror() *url.URL    { return AlpineMirror.Get() }
func ResetAlpineMirror()           { AlpineMirror.Reset() }
