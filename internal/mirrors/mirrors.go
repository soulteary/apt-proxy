package mirrors

import (
	"regexp"
	"strings"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// builtinMirrorURLs converts distro UrlWithAlias list to full URL strings (single source for built-in mirrors).
func builtinMirrorURLs(mirrors []distro.UrlWithAlias) []string {
	out := make([]string, 0, len(mirrors))
	for _, m := range mirrors {
		out = append(out, GetFullMirrorURL(m))
	}
	return out
}

func GetGeoMirrorUrlsByMode(mode int) (mirrors []string) {
	// Prefer registry (config-loaded) mirrors when present
	if reg := distro.GetRegistry(); reg != nil {
		if d, ok := reg.GetByType(mode); ok && len(d.Mirrors) > 0 {
			for _, m := range d.Mirrors {
				mirrors = append(mirrors, GetFullMirrorURL(m))
			}
			if len(mirrors) > 0 {
				return mirrors
			}
		}
	}

	if mode == distro.TYPE_LINUX_DISTROS_UBUNTU {
		ubuntuMirrorsOnline, err := GetUbuntuMirrorUrlsByGeo()
		if err != nil {
			return builtinMirrorURLs(distro.BUILDIN_UBUNTU_MIRRORS)
		}
		return ubuntuMirrorsOnline
	}

	if mode == distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS {
		ubuntuPortsMirrorsOnline, err := GetUbuntuMirrorUrlsByGeo()
		if err != nil {
			return builtinMirrorURLs(distro.BUILDIN_UBUNTU_PORTS_MIRRORS)
		}

		results := make([]string, 0, len(ubuntuPortsMirrorsOnline))
		for _, mirror := range ubuntuPortsMirrorsOnline {
			results = append(results, strings.ReplaceAll(mirror, "/ubuntu/", "/ubuntu-ports/"))
		}
		return results
	}

	if mode == distro.TYPE_LINUX_DISTROS_DEBIAN {
		return builtinMirrorURLs(distro.BUILDIN_DEBIAN_MIRRORS)
	}

	if mode == distro.TYPE_LINUX_DISTROS_CENTOS {
		return builtinMirrorURLs(distro.BUILDIN_CENTOS_MIRRORS)
	}

	if mode == distro.TYPE_LINUX_DISTROS_ALPINE {
		return builtinMirrorURLs(distro.BUILDIN_ALPINE_MIRRORS)
	}

	mirrors = append(mirrors, builtinMirrorURLs(distro.BUILDIN_UBUNTU_MIRRORS)...)
	mirrors = append(mirrors, builtinMirrorURLs(distro.BUILDIN_UBUNTU_PORTS_MIRRORS)...)
	mirrors = append(mirrors, builtinMirrorURLs(distro.BUILDIN_DEBIAN_MIRRORS)...)
	mirrors = append(mirrors, builtinMirrorURLs(distro.BUILDIN_CENTOS_MIRRORS)...)
	mirrors = append(mirrors, builtinMirrorURLs(distro.BUILDIN_ALPINE_MIRRORS)...)
	return mirrors
}

func GetFullMirrorURL(mirror distro.UrlWithAlias) string {
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

// normalizeAliasURL ensures the alias value is a full URL (adds https:// if missing)
func normalizeAliasURL(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	u, err := BuildHTTPSURL(raw)
	if err != nil {
		return "https://" + raw
	}
	return u
}

func GetMirrorURLByAliases(osType int, alias string) string {
	// Prefer registry (config-loaded) aliases when present
	if reg := distro.GetRegistry(); reg != nil {
		if d, ok := reg.GetByType(osType); ok && len(d.Aliases) > 0 {
			if u, ok := d.Aliases[alias]; ok {
				return normalizeAliasURL(u)
			}
			// Support "cn:tsinghua" by stripping "cn:" prefix
			if strings.HasPrefix(alias, "cn:") {
				if u, ok := d.Aliases[strings.TrimPrefix(alias, "cn:")]; ok {
					return normalizeAliasURL(u)
				}
			}
		}
	}

	switch osType {
	case distro.TYPE_LINUX_DISTROS_UBUNTU:
		for _, mirror := range distro.BUILDIN_UBUNTU_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		for _, mirror := range distro.BUILDIN_UBUNTU_PORTS_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case distro.TYPE_LINUX_DISTROS_DEBIAN:
		for _, mirror := range distro.BUILDIN_DEBIAN_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case distro.TYPE_LINUX_DISTROS_CENTOS:
		for _, mirror := range distro.BUILDIN_CENTOS_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	case distro.TYPE_LINUX_DISTROS_ALPINE:
		for _, mirror := range distro.BUILDIN_ALPINE_MIRRORS {
			if mirror.Alias == alias {
				return GetFullMirrorURL(mirror)
			}
		}
	}
	return ""
}

func GetPredefinedConfiguration(proxyMode int) (string, *regexp.Regexp) {
	if reg := distro.GetRegistry(); reg != nil {
		if d, ok := reg.GetByType(proxyMode); ok && d.URLPattern != nil {
			return d.BenchmarkURL, d.URLPattern
		}
	}
	switch proxyMode {
	case distro.TYPE_LINUX_DISTROS_UBUNTU:
		return distro.UBUNTU_BENCHMARK_URL, distro.UBUNTU_HOST_PATTERN
	case distro.TYPE_LINUX_DISTROS_UBUNTU_PORTS:
		return distro.UBUNTU_PORTS_BENCHMARK_URL, distro.UBUNTU_PORTS_HOST_PATTERN
	case distro.TYPE_LINUX_DISTROS_DEBIAN:
		return distro.DEBIAN_BENCHMARK_URL, distro.DEBIAN_HOST_PATTERN
	case distro.TYPE_LINUX_DISTROS_CENTOS:
		return distro.CENTOS_BENCHMARK_URL, distro.CENTOS_HOST_PATTERN
	case distro.TYPE_LINUX_DISTROS_ALPINE:
		return distro.ALPINE_BENCHMARK_URL, distro.ALPINE_HOST_PATTERN
	}
	return "", nil
}
