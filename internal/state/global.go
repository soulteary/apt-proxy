// Package state provides application state management with support for both
// global singleton access (backward compatibility) and dependency injection patterns.
package state

import (
	"net/url"
	"sync/atomic"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/internal/distro"
	mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

// MirrorState manages mirror URL states for a specific distribution.
//
// The URL is stored as an atomic.Pointer[url.URL]. Reads (the hot path
// for every proxied request) are lock-free; writes (Set/Reset) replace the
// pointer atomically. We never mutate a *url.URL after publishing it, so
// callers can hold the returned pointer without racing future writers.
type MirrorState struct {
	url      atomic.Pointer[url.URL]
	distType int
}

// NewMirrorState creates a new MirrorState instance
func NewMirrorState(distType int) *MirrorState {
	return &MirrorState{
		distType: distType,
	}
}

// Set updates the mirror URL.
// An empty input or unparseable URL clears the state (and logs a warning
// for the latter so misconfigurations are surfaced).
func (m *MirrorState) Set(input string) {
	if input == "" {
		m.url.Store(nil)
		return
	}

	mirror := input
	if alias := mirrors.GetMirrorURLByAliases(m.distType, input); alias != "" {
		mirror = alias
	}

	parsed, err := url.Parse(mirror)
	if err != nil {
		logger.Default().Warn().
			Err(err).
			Int("dist_type", m.distType).
			Str("input", input).
			Msg("invalid mirror URL, clearing state")
		m.url.Store(nil)
		return
	}
	m.url.Store(parsed)
}

// Get returns the current mirror URL.
// Callers receive the same *url.URL the writer published; never mutate it.
func (m *MirrorState) Get() *url.URL {
	return m.url.Load()
}

// Reset clears the mirror URL.
func (m *MirrorState) Reset() {
	m.url.Store(nil)
}

// Clone creates a copy of the MirrorState containing a deep copy of the URL.
func (m *MirrorState) Clone() *MirrorState {
	clone := NewMirrorState(m.distType)
	if cur := m.url.Load(); cur != nil {
		urlCopy := *cur
		clone.url.Store(&urlCopy)
	}
	return clone
}

// AppState manages the complete application state including proxy mode
// and mirror configurations for all supported distributions.
// This struct supports dependency injection for better testability.
type AppState struct {
	proxyMode   atomic.Int64 // proxy mode; atomic so callers can both read and write without locks
	Ubuntu      *MirrorState
	UbuntuPorts *MirrorState
	Debian      *MirrorState
	CentOS      *MirrorState
	Alpine      *MirrorState
}

// NewAppState creates a new AppState instance with initialized mirror states
func NewAppState() *AppState {
	return &AppState{
		Ubuntu:      NewMirrorState(distro.TypeUbuntu),
		UbuntuPorts: NewMirrorState(distro.TypeUbuntuPorts),
		Debian:      NewMirrorState(distro.TypeDebian),
		CentOS:      NewMirrorState(distro.TypeCentOS),
		Alpine:      NewMirrorState(distro.TypeAlpine),
	}
}

// SetProxyMode sets the proxy mode
func (s *AppState) SetProxyMode(mode int) {
	s.proxyMode.Store(int64(mode))
}

// GetProxyMode returns the current proxy mode
func (s *AppState) GetProxyMode() int {
	return int(s.proxyMode.Load())
}

// SetMirror sets the mirror URL for a specific distribution
func (s *AppState) SetMirror(distType int, input string) {
	if state := s.mirrorByType(distType); state != nil {
		state.Set(input)
	}
}

// GetMirror returns the mirror URL for a specific distribution
func (s *AppState) GetMirror(distType int) *url.URL {
	if state := s.mirrorByType(distType); state != nil {
		return state.Get()
	}
	return nil
}

// mirrorByType returns the *MirrorState backing the given distro type, or
// nil for unknown types. Centralising the switch avoids drift between
// SetMirror/GetMirror/ResetAll's parallel cases.
func (s *AppState) mirrorByType(distType int) *MirrorState {
	switch distType {
	case distro.TypeUbuntu:
		return s.Ubuntu
	case distro.TypeUbuntuPorts:
		return s.UbuntuPorts
	case distro.TypeDebian:
		return s.Debian
	case distro.TypeCentOS:
		return s.CentOS
	case distro.TypeAlpine:
		return s.Alpine
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
	clone := &AppState{
		Ubuntu:      s.Ubuntu.Clone(),
		UbuntuPorts: s.UbuntuPorts.Clone(),
		Debian:      s.Debian.Clone(),
		CentOS:      s.CentOS.Clone(),
		Alpine:      s.Alpine.Clone(),
	}
	clone.proxyMode.Store(s.proxyMode.Load())
	return clone
}

// ============================================================================
// Global Singleton - Backward Compatibility Layer
// ============================================================================

// globalState holds the process-wide AppState. We use atomic.Pointer instead
// of a sync.Once + sync.RWMutex pair so reads (every proxied request goes
// through GetUbuntuMirror et al.) are lock-free.
var globalState atomic.Pointer[AppState]

// initGlobalState lazily creates the singleton on first access. CAS guards
// against the rare case of two goroutines both observing nil at startup.
func initGlobalState() *AppState {
	if cur := globalState.Load(); cur != nil {
		return cur
	}
	fresh := NewAppState()
	if globalState.CompareAndSwap(nil, fresh) {
		return fresh
	}
	return globalState.Load()
}

// Global returns the global AppState instance.
// This is primarily for dependency injection scenarios where you need
// to pass the state explicitly.
func Global() *AppState {
	return initGlobalState()
}

// SetGlobal replaces the global AppState instance.
// This is useful for testing or when you want to use a custom state instance.
// Safe to call concurrently with Global().
func SetGlobal(state *AppState) {
	globalState.Store(state)
}

// ============================================================================
// Backward Compatibility Functions - Proxy Mode
// ============================================================================

func SetProxyMode(mode int) {
	initGlobalState().SetProxyMode(mode)
}

func GetProxyMode() int {
	return initGlobalState().GetProxyMode()
}

// ============================================================================
// Backward Compatibility Functions - Mirror States (Global Variables Pattern)
// ============================================================================

// These variables maintain backward compatibility with existing code
// that accesses mirrors directly. They delegate to the global AppState.
var (
	UbuntuMirror      = &mirrorProxy{distType: distro.TypeUbuntu}
	UbuntuPortsMirror = &mirrorProxy{distType: distro.TypeUbuntuPorts}
	DebianMirror      = &mirrorProxy{distType: distro.TypeDebian}
	CentOSMirror      = &mirrorProxy{distType: distro.TypeCentOS}
	AlpineMirror      = &mirrorProxy{distType: distro.TypeAlpine}
)

// mirrorProxy provides a proxy to the global state's mirror states
type mirrorProxy struct {
	distType int
}

func (p *mirrorProxy) Set(input string) {
	initGlobalState().SetMirror(p.distType, input)
}

func (p *mirrorProxy) Get() *url.URL {
	return initGlobalState().GetMirror(p.distType)
}

func (p *mirrorProxy) Reset() {
	if state := initGlobalState().mirrorByType(p.distType); state != nil {
		state.Reset()
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
