package state

import (
	"net/url"

	Define "github.com/soulteary/apt-proxy/define"
	Mirrors "github.com/soulteary/apt-proxy/internal/mirrors"
)

var ALPINE_MIRROR *url.URL

func SetAlpineMirror(input string) {
	if input == "" {
		ALPINE_MIRROR = nil
		return
	}

	mirror := input
	alias := Mirrors.GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_ALPINE, input)
	if alias != "" {
		mirror = alias
	}

	url, err := url.Parse(mirror)
	if err != nil {
		ALPINE_MIRROR = nil
		return
	}
	ALPINE_MIRROR = url
}

func GetAlpineMirror() *url.URL {
	return ALPINE_MIRROR
}

func ResetAlpineMirror() {
	ALPINE_MIRROR = nil
}
