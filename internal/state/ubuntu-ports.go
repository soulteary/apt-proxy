package state

import (
	"net/url"

	Define "github.com/soulteary/apt-proxy/define"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

var UBUNTU_PORTS_MIRROR *url.URL

func SetUbuntuPortsMirror(input string) {
	if input == "" {
		UBUNTU_PORTS_MIRROR = nil
		return
	}

	mirror := input
	alias := Mirrors.GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS, input)
	if alias != "" {
		mirror = alias
	}

	url, err := url.Parse(mirror)
	if err != nil {
		UBUNTU_PORTS_MIRROR = nil
		return
	}
	UBUNTU_PORTS_MIRROR = url
}

func GetUbuntuPortsMirror() *url.URL {
	return UBUNTU_PORTS_MIRROR
}

func ResetUbuntuPortsMirror() {
	UBUNTU_PORTS_MIRROR = nil
}
