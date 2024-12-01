package state

import (
	"net/url"

	Define "github.com/soulteary/apt-proxy/define"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

var DEBIAN_MIRROR *url.URL

func SetDebianMirror(input string) {
	if input == "" {
		DEBIAN_MIRROR = nil
		return
	}

	mirror := input
	alias := Mirrors.GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_DEBIAN, input)
	if alias != "" {
		mirror = alias
	}

	url, err := url.Parse(mirror)
	if err != nil {
		DEBIAN_MIRROR = nil
		return
	}
	DEBIAN_MIRROR = url
}

func GetDebianMirror() *url.URL {
	return DEBIAN_MIRROR
}

func ResetDebianMirror() {
	DEBIAN_MIRROR = nil
}
