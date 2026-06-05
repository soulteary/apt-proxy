package mirrors

import (
	"regexp"
	"strings"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// builtinMirrorURLs converts distro URLWithAlias list to full URL strings (single source for built-in mirrors).
func builtinMirrorURLs(mirrors []distro.URLWithAlias) []string {
	out := make([]string, 0, len(mirrors))
	for _, m := range mirrors {
		out = append(out, GetFullMirrorURL(m))
	}
	return out
}

// builtinDistro consolidates the per-mode built-in metadata so the switch
// blocks below collapse into a single table-driven lookup.
type builtinDistro struct {
	mirrors      []distro.URLWithAlias
	benchmarkURL string
	hostPattern  *regexp.Regexp
}

// builtinByMode is the single source of truth for built-in (compile-time)
// mirror metadata, keyed by distro type. Registry-loaded data overrides this
// at runtime.
var builtinByMode = map[int]builtinDistro{
	distro.TypeUbuntu: {
		mirrors:      distro.BuiltinUbuntuMirrors,
		benchmarkURL: distro.UbuntuBenchmarkURL,
		hostPattern:  distro.UbuntuHostPattern,
	},
	distro.TypeUbuntuPorts: {
		mirrors:      distro.BuiltinUbuntuPortsMirrors,
		benchmarkURL: distro.UbuntuPortsBenchmarkURL,
		hostPattern:  distro.UbuntuPortsHostPattern,
	},
	distro.TypeDebian: {
		mirrors:      distro.BuiltinDebianMirrors,
		benchmarkURL: distro.DebianBenchmarkURL,
		hostPattern:  distro.DebianHostPattern,
	},
	distro.TypeCentOS: {
		mirrors:      distro.BuiltinCentosMirrors,
		benchmarkURL: distro.CentosBenchmarkURL,
		hostPattern:  distro.CentosHostPattern,
	},
	distro.TypeAlpine: {
		mirrors:      distro.BuiltinAlpineMirrors,
		benchmarkURL: distro.AlpineBenchmarkURL,
		hostPattern:  distro.AlpineHostPattern,
	},
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

	// Ubuntu/UbuntuPorts try geo-based mirrors first, fall back to built-in.
	if mode == distro.TypeUbuntu || mode == distro.TypeUbuntuPorts {
		online, err := GetUbuntuMirrorUrlsByGeo()
		if err != nil {
			return builtinMirrorURLs(builtinByMode[mode].mirrors)
		}
		if mode == distro.TypeUbuntu {
			return online
		}
		// Ubuntu Ports re-uses the Ubuntu geo list with a path swap.
		results := make([]string, 0, len(online))
		for _, m := range online {
			results = append(results, strings.ReplaceAll(m, "/ubuntu/", "/ubuntu-ports/"))
		}
		return results
	}

	// Other single-distro modes: just return their built-in list.
	if b, ok := builtinByMode[mode]; ok {
		return builtinMirrorURLs(b.mirrors)
	}

	// Fallback: aggregate all known built-in mirrors (used by ALL_DISTROS).
	for _, b := range builtinByMode {
		mirrors = append(mirrors, builtinMirrorURLs(b.mirrors)...)
	}
	return mirrors
}

func GetFullMirrorURL(mirror distro.URLWithAlias) string {
	if mirror.HTTP {
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
	if mirror.HTTPS {
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

	if b, ok := builtinByMode[osType]; ok {
		for _, m := range b.mirrors {
			if m.Alias == alias {
				return GetFullMirrorURL(m)
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
	if b, ok := builtinByMode[proxyMode]; ok {
		return b.benchmarkURL, b.hostPattern
	}
	return "", nil
}
