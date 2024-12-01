package mirrors

import (
	"regexp"
	"strings"

	Define "github.com/soulteary/apt-proxy/define"
)

func GenerateMirrorListByPredefined(osType int) (mirrors []string) {
	var src []Define.UrlWithAlias
	switch osType {
	case Define.TYPE_LINUX_ALL_DISTROS:
		src = append(src, Define.BUILDIN_UBUNTU_MIRRORS...)
		src = append(src, Define.BUILDIN_UBUNTU_PORTS_MIRRORS...)
		src = append(src, Define.BUILDIN_DEBIAN_MIRRORS...)
		src = append(src, Define.BUILDIN_CENTOS_MIRRORS...)
		src = append(src, Define.BUILDIN_ALPINE_MIRRORS...)
	case Define.TYPE_LINUX_DISTROS_UBUNTU:
		src = Define.BUILDIN_UBUNTU_MIRRORS
	case Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		src = Define.BUILDIN_UBUNTU_PORTS_MIRRORS
	case Define.TYPE_LINUX_DISTROS_DEBIAN:
		src = Define.BUILDIN_DEBIAN_MIRRORS
	case Define.TYPE_LINUX_DISTROS_CENTOS:
		src = Define.BUILDIN_CENTOS_MIRRORS
	case Define.TYPE_LINUX_DISTROS_ALPINE:
		src = Define.BUILDIN_ALPINE_MIRRORS
	}

	for _, mirror := range src {
		mirrors = append(mirrors, mirror.URL)
	}
	return mirrors
}

var BUILDIN_UBUNTU_MIRRORS = GenerateMirrorListByPredefined(Define.TYPE_LINUX_DISTROS_UBUNTU)
var BUILDIN_UBUNTU_PORTS_MIRRORS = GenerateMirrorListByPredefined(Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
var BUILDIN_DEBIAN_MIRRORS = GenerateMirrorListByPredefined(Define.TYPE_LINUX_DISTROS_DEBIAN)
var BUILDIN_CENTOS_MIRRORS = GenerateMirrorListByPredefined(Define.TYPE_LINUX_DISTROS_CENTOS)
var BUILDIN_ALPINE_MIRRORS = GenerateMirrorListByPredefined(Define.TYPE_LINUX_DISTROS_ALPINE)

func GetGeoMirrorUrlsByMode(mode int) (mirrors []string) {
	if mode == Define.TYPE_LINUX_DISTROS_UBUNTU {
		ubuntuMirrorsOnline, err := GetUbuntuMirrorUrlsByGeo()
		if err != nil {
			return BUILDIN_UBUNTU_MIRRORS
		}
		return ubuntuMirrorsOnline
	}

	if mode == Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS {
		ubuntuPortsMirrorsOnline, err := GetUbuntuMirrorUrlsByGeo()
		if err != nil {
			return BUILDIN_UBUNTU_PORTS_MIRRORS
		}

		results := make([]string, 0, len(ubuntuPortsMirrorsOnline))
		for _, mirror := range ubuntuPortsMirrorsOnline {
			results = append(results, strings.ReplaceAll(mirror, "/ubuntu/", "/ubuntu-ports/"))
		}
		return results
	}

	if mode == Define.TYPE_LINUX_DISTROS_DEBIAN {
		return BUILDIN_DEBIAN_MIRRORS
	}

	if mode == Define.TYPE_LINUX_DISTROS_CENTOS {
		return BUILDIN_CENTOS_MIRRORS
	}

	if mode == Define.TYPE_LINUX_DISTROS_ALPINE {
		return BUILDIN_ALPINE_MIRRORS
	}

	mirrors = append(mirrors, BUILDIN_UBUNTU_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_UBUNTU_PORTS_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_DEBIAN_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_CENTOS_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_ALPINE_MIRRORS...)
	return mirrors
}

func GetFullMirrorURL(mirror Define.UrlWithAlias) string {
	if mirror.Http {
		if strings.HasPrefix(mirror.URL, "http://") {
			return mirror.URL
		}
		return "http://" + mirror.URL
	}
	if mirror.Https {
		if strings.HasPrefix(mirror.URL, "https://") {
			return mirror.URL
		}
		return "https://" + mirror.URL
	}
	return "https://" + mirror.URL
}

func GetMirrorURLByAliases(osType int, alias string) string {
	switch osType {
	case Define.TYPE_LINUX_DISTROS_UBUNTU:
		for _, mirror := range Define.BUILDIN_UBUNTU_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		for _, mirror := range Define.BUILDIN_UBUNTU_PORTS_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case Define.TYPE_LINUX_DISTROS_DEBIAN:
		for _, mirror := range Define.BUILDIN_DEBIAN_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case Define.TYPE_LINUX_DISTROS_CENTOS:
		for _, mirror := range Define.BUILDIN_CENTOS_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case Define.TYPE_LINUX_DISTROS_ALPINE:
		for _, mirror := range Define.BUILDIN_ALPINE_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	}
	return ""
}

func GetPredefinedConfiguration(proxyMode int) (string, *regexp.Regexp) {
	switch proxyMode {
	case Define.TYPE_LINUX_DISTROS_UBUNTU:
		return Define.UBUNTU_BENCHMAKR_URL, Define.UBUNTU_HOST_PATTERN
	case Define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return Define.UBUNTU_PORTS_BENCHMAKR_URL, Define.UBUNTU_PORTS_HOST_PATTERN
	case Define.TYPE_LINUX_DISTROS_DEBIAN:
		return Define.DEBIAN_BENCHMAKR_URL, Define.DEBIAN_HOST_PATTERN
	case Define.TYPE_LINUX_DISTROS_CENTOS:
		return Define.CENTOS_BENCHMAKR_URL, Define.CENTOS_HOST_PATTERN
	case Define.TYPE_LINUX_DISTROS_ALPINE:
		return Define.ALPINE_BENCHMAKR_URL, Define.ALPINE_HOST_PATTERN
	}
	return "", nil
}
