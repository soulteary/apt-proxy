package mirrors

import (
	"regexp"
	"strings"

	define "github.com/soulteary/apt-proxy/define"
)

func GenerateMirrorListByPredefined(osType int) (mirrors []string) {
	var src []define.UrlWithAlias
	switch osType {
	case define.TYPE_LINUX_ALL_DISTROS:
		src = append(src, define.BUILDIN_UBUNTU_MIRRORS...)
		src = append(src, define.BUILDIN_UBUNTU_PORTS_MIRRORS...)
		src = append(src, define.BUILDIN_DEBIAN_MIRRORS...)
		src = append(src, define.BUILDIN_CENTOS_MIRRORS...)
		src = append(src, define.BUILDIN_ALPINE_MIRRORS...)
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		src = define.BUILDIN_UBUNTU_MIRRORS
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		src = define.BUILDIN_UBUNTU_PORTS_MIRRORS
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		src = define.BUILDIN_DEBIAN_MIRRORS
	case define.TYPE_LINUX_DISTROS_CENTOS:
		src = define.BUILDIN_CENTOS_MIRRORS
	case define.TYPE_LINUX_DISTROS_ALPINE:
		src = define.BUILDIN_ALPINE_MIRRORS
	}

	for _, mirror := range src {
		mirrors = append(mirrors, mirror.URL)
	}
	return mirrors
}

var BUILDIN_UBUNTU_MIRRORS = GenerateMirrorListByPredefined(define.TYPE_LINUX_DISTROS_UBUNTU)
var BUILDIN_UBUNTU_PORTS_MIRRORS = GenerateMirrorListByPredefined(define.TYPE_LINUX_DISTROS_UBUNTU_PORTS)
var BUILDIN_DEBIAN_MIRRORS = GenerateMirrorListByPredefined(define.TYPE_LINUX_DISTROS_DEBIAN)
var BUILDIN_CENTOS_MIRRORS = GenerateMirrorListByPredefined(define.TYPE_LINUX_DISTROS_CENTOS)
var BUILDIN_ALPINE_MIRRORS = GenerateMirrorListByPredefined(define.TYPE_LINUX_DISTROS_ALPINE)

func GetGeoMirrorUrlsByMode(mode int) (mirrors []string) {
	if mode == define.TYPE_LINUX_DISTROS_UBUNTU {
		ubuntuMirrorsOnline, err := GetUbuntuMirrorUrlsByGeo()
		if err != nil {
			return BUILDIN_UBUNTU_MIRRORS
		}
		return ubuntuMirrorsOnline
	}

	if mode == define.TYPE_LINUX_DISTROS_UBUNTU_PORTS {
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

	if mode == define.TYPE_LINUX_DISTROS_DEBIAN {
		return BUILDIN_DEBIAN_MIRRORS
	}

	if mode == define.TYPE_LINUX_DISTROS_CENTOS {
		return BUILDIN_CENTOS_MIRRORS
	}

	if mode == define.TYPE_LINUX_DISTROS_ALPINE {
		return BUILDIN_ALPINE_MIRRORS
	}

	mirrors = append(mirrors, BUILDIN_UBUNTU_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_UBUNTU_PORTS_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_DEBIAN_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_CENTOS_MIRRORS...)
	mirrors = append(mirrors, BUILDIN_ALPINE_MIRRORS...)
	return mirrors
}

func GetFullMirrorURL(mirror define.UrlWithAlias) string {
	if mirror.Http {
		if strings.HasPrefix(mirror.URL, "http://") {
			return mirror.URL
		}
		url, err := BuildHTTPURL(mirror.URL)
		if err != nil {
			// Fallback to concatenation if template fails
			return "http://" + mirror.URL
		}
		return url
	}
	if mirror.Https {
		if strings.HasPrefix(mirror.URL, "https://") {
			return mirror.URL
		}
		url, err := BuildHTTPSURL(mirror.URL)
		if err != nil {
			// Fallback to concatenation if template fails
			return "https://" + mirror.URL
		}
		return url
	}
	url, err := BuildHTTPSURL(mirror.URL)
	if err != nil {
		// Fallback to concatenation if template fails
		return "https://" + mirror.URL
	}
	return url
}

func GetMirrorURLByAliases(osType int, alias string) string {
	switch osType {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		for _, mirror := range define.BUILDIN_UBUNTU_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		for _, mirror := range define.BUILDIN_UBUNTU_PORTS_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		for _, mirror := range define.BUILDIN_DEBIAN_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case define.TYPE_LINUX_DISTROS_CENTOS:
		for _, mirror := range define.BUILDIN_CENTOS_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case define.TYPE_LINUX_DISTROS_ALPINE:
		for _, mirror := range define.BUILDIN_ALPINE_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	}
	return ""
}

func GetPredefinedConfiguration(proxyMode int) (string, *regexp.Regexp) {
	switch proxyMode {
	case define.TYPE_LINUX_DISTROS_UBUNTU:
		return define.UBUNTU_BENCHMAKR_URL, define.UBUNTU_HOST_PATTERN
	case define.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return define.UBUNTU_PORTS_BENCHMAKR_URL, define.UBUNTU_PORTS_HOST_PATTERN
	case define.TYPE_LINUX_DISTROS_DEBIAN:
		return define.DEBIAN_BENCHMAKR_URL, define.DEBIAN_HOST_PATTERN
	case define.TYPE_LINUX_DISTROS_CENTOS:
		return define.CENTOS_BENCHMAKR_URL, define.CENTOS_HOST_PATTERN
	case define.TYPE_LINUX_DISTROS_ALPINE:
		return define.ALPINE_BENCHMAKR_URL, define.ALPINE_HOST_PATTERN
	}
	return "", nil
}
