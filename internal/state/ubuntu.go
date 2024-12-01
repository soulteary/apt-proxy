package state

import (
	"net/url"

	Define "github.com/soulteary/apt-proxy/define"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

var UBUNTU_MIRROR *url.URL

func SetUbuntuMirror(input string) {
	if input == "" {
		UBUNTU_MIRROR = nil
		return
	}

	mirror := input
	alias := Mirrors.GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_UBUNTU, input)
	if alias != "" {
		mirror = alias
	}

	url, err := url.Parse(mirror)
	if err != nil {
		UBUNTU_MIRROR = nil
		return
	}
	UBUNTU_MIRROR = url
}

func GetUbuntuMirror() *url.URL {
	return UBUNTU_MIRROR
}

func ResetUbuntuMirror() {
	UBUNTU_MIRROR = nil
}
