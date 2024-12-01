package state

import (
	"net/url"

	Define "github.com/soulteary/apt-proxy/define"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

var CENTOS_MIRROR *url.URL

func SetCentOSMirror(input string) {
	if input == "" {
		CENTOS_MIRROR = nil
		return
	}

	mirror := input
	alias := Mirrors.GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_CENTOS, input)
	if alias != "" {
		mirror = alias
	}

	url, err := url.Parse(mirror)
	if err != nil {
		CENTOS_MIRROR = nil
		return
	}
	CENTOS_MIRROR = url
}

func GetCentOSMirror() *url.URL {
	return CENTOS_MIRROR
}

func ResetCentOSMirror() {
	CENTOS_MIRROR = nil
}
