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

// Package state provides the per-Server runtime state container.
//
// AppState is a pure value type: every Server constructs one via
// NewAppState and threads it through the components that need to read
// the proxy mode or the per-distro mirror URLs. There is no package-level
// singleton; multiple Server instances in the same process can hold
// independent AppState values and never collide on writes.
package state

import (
	"net/url"
	"sync/atomic"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/internal/distro"
	mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

// MirrorState manages the mirror URL for a specific distribution.
//
// The URL is stored as an atomic.Pointer[url.URL]. Reads (the hot path
// for every proxied request) are lock-free; writes (Set/Reset) replace the
// pointer atomically. We never mutate a *url.URL after publishing it, so
// callers can hold the returned pointer without racing future writers.
type MirrorState struct {
	url      atomic.Pointer[url.URL]
	distType int
}

// NewMirrorState creates a new MirrorState for the given distro type.
func NewMirrorState(distType int) *MirrorState {
	return &MirrorState{distType: distType}
}

// Set updates the mirror URL.
//
// An empty input or an unparseable URL clears the state (and logs a
// warning for the latter so misconfigurations are surfaced).
func (m *MirrorState) Set(input string) {
	m.SetWithRegistry(input, nil)
}

// SetWithRegistry updates the mirror URL, resolving aliases against the
// supplied registry when one is provided. The nil-registry branch keeps
// the simple Set call usable in tests / callers that do not need alias
// resolution.
func (m *MirrorState) SetWithRegistry(input string, reg *distro.Registry) {
	if input == "" {
		m.url.Store(nil)
		return
	}

	mirror := input
	if alias := mirrors.GetMirrorURLByAliases(reg, m.distType, input); alias != "" {
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

// Get returns the current mirror URL, or nil if unset.
//
// Callers receive the same *url.URL the writer published; never mutate it.
func (m *MirrorState) Get() *url.URL {
	return m.url.Load()
}

// Reset clears the mirror URL.
func (m *MirrorState) Reset() {
	m.url.Store(nil)
}

// Clone returns an independent MirrorState that contains a deep copy of
// the stored URL.
func (m *MirrorState) Clone() *MirrorState {
	clone := NewMirrorState(m.distType)
	if cur := m.url.Load(); cur != nil {
		urlCopy := *cur
		clone.url.Store(&urlCopy)
	}
	return clone
}

// AppState manages the complete runtime state for a single Server: the
// proxy mode and the mirror configuration for every supported distro.
type AppState struct {
	proxyMode   atomic.Int64
	Ubuntu      *MirrorState
	UbuntuPorts *MirrorState
	Debian      *MirrorState
	CentOS      *MirrorState
	Alpine      *MirrorState
}

// NewAppState constructs a fresh AppState with empty MirrorStates for
// every supported distro.
func NewAppState() *AppState {
	return &AppState{
		Ubuntu:      NewMirrorState(distro.TypeUbuntu),
		UbuntuPorts: NewMirrorState(distro.TypeUbuntuPorts),
		Debian:      NewMirrorState(distro.TypeDebian),
		CentOS:      NewMirrorState(distro.TypeCentOS),
		Alpine:      NewMirrorState(distro.TypeAlpine),
	}
}

// SetProxyMode sets the active proxy mode (one of distro.Type*).
func (s *AppState) SetProxyMode(mode int) {
	s.proxyMode.Store(int64(mode))
}

// GetProxyMode returns the active proxy mode.
func (s *AppState) GetProxyMode() int {
	return int(s.proxyMode.Load())
}

// SetMirror sets the mirror URL for a specific distro type. Unknown
// types are ignored.
func (s *AppState) SetMirror(distType int, input string) {
	s.SetMirrorWithRegistry(distType, input, nil)
}

// SetMirrorWithRegistry behaves like SetMirror but resolves aliases via
// the supplied registry.
func (s *AppState) SetMirrorWithRegistry(distType int, input string, reg *distro.Registry) {
	if state := s.mirrorByType(distType); state != nil {
		state.SetWithRegistry(input, reg)
	}
}

// GetMirror returns the mirror URL for a specific distro type, or nil
// when unset / unknown.
func (s *AppState) GetMirror(distType int) *url.URL {
	if state := s.mirrorByType(distType); state != nil {
		return state.Get()
	}
	return nil
}

// mirrorByType returns the *MirrorState backing the given distro type,
// or nil for unknown types. Centralising the switch avoids drift between
// SetMirror/GetMirror/ResetAll.
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

// ResetAll clears every per-distro mirror state.
func (s *AppState) ResetAll() {
	s.Ubuntu.Reset()
	s.UbuntuPorts.Reset()
	s.Debian.Reset()
	s.CentOS.Reset()
	s.Alpine.Reset()
}

// Clone returns a deep copy of the AppState. The clone shares no
// mutable state with the original.
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
