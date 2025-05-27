package state

import (
	"net/url"
	"sync"

	Define "github.com/apham0001/apt-proxy/define"
	Mirrors "github.com/apham0001/apt-proxy/internal/mirrors"
)

var (
	proxyMode int
	modeMutex sync.RWMutex
)

func SetProxyMode(mode int) {
	modeMutex.Lock()
	defer modeMutex.Unlock()
	proxyMode = mode
}

func GetProxyMode() int {
	modeMutex.RLock()
	defer modeMutex.RUnlock()
	return proxyMode
}

// MirrorState manages mirror URL states
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
	if alias := Mirrors.GetMirrorURLByAliases(m.distType, input); alias != "" {
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

var (
	// Mirror states for different distributions
	UbuntuMirror      		= NewMirrorState(Define.TYPE_LINUX_DISTROS_UBUNTU)
	UbuntuPortsMirror 		= NewMirrorState(Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
	DebianMirror      		= NewMirrorState(Define.TYPE_LINUX_DISTROS_DEBIAN)
	DebianSecurityMirror 	= NewMirrorState(Define.TYPE_LINUX_DISTROS_DEBIAN_SECURITY)
	CentOSMirror      		= NewMirrorState(Define.TYPE_LINUX_DISTROS_CENTOS)
	AlpineMirror      		= NewMirrorState(Define.TYPE_LINUX_DISTROS_ALPINE)
)

// Convenience functions for backward compatibility
func SetUbuntuMirror(input string) { UbuntuMirror.Set(input) }
func GetUbuntuMirror() *url.URL    { return UbuntuMirror.Get() }
func ResetUbuntuMirror()           { UbuntuMirror.Reset() }

func SetUbuntuPortsMirror(input string) { UbuntuPortsMirror.Set(input) }
func GetUbuntuPortsMirror() *url.URL    { return UbuntuPortsMirror.Get() }
func ResetUbuntuPortsMirror()           { UbuntuPortsMirror.Reset() }

func SetDebianMirror(input string) { DebianMirror.Set(input) }
func GetDebianMirror() *url.URL    { return DebianMirror.Get() }
func ResetDebianMirror()           { DebianMirror.Reset() }

func SetDebianSecurityMirror(input string) { DebianSecurityMirror.Set(input) }
func GetDebianSecurityMirror() *url.URL    { return DebianSecurityMirror.Get() }
func ResetDebianSecurityMirror()           { DebianSecurityMirror.Reset() }

func SetCentOSMirror(input string) { CentOSMirror.Set(input) }
func GetCentOSMirror() *url.URL    { return CentOSMirror.Get() }
func ResetCentOSMirror()           { CentOSMirror.Reset() }

func SetAlpineMirror(input string) { AlpineMirror.Set(input) }
func GetAlpineMirror() *url.URL    { return AlpineMirror.Get() }
func ResetAlpineMirror()           { AlpineMirror.Reset() }
