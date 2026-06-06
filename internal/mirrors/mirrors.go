// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// GetGeoMirrorUrlsByMode returns the candidate upstream mirror URLs
// for the given proxy mode. When reg is non-nil, registry-loaded
// mirrors are preferred over the compile-time built-ins.
func GetGeoMirrorUrlsByMode(reg *distro.Registry, mode int) (mirrors []string) {
	// Ubuntu/UbuntuPorts: prefer geo-derived mirrors (real point of the
	// `mirrors.txt` lookup). Fall back to registry/built-in on failure so
	// the proxy still has *some* upstream list when the geo API is down.
	if mode == distro.TypeUbuntu || mode == distro.TypeUbuntuPorts {
		online, err := GetUbuntuMirrorUrlsByGeo()
		if err == nil && len(online) > 0 {
			if mode == distro.TypeUbuntu {
				return online
			}
			results := make([]string, 0, len(online))
			for _, m := range online {
				results = append(results, strings.ReplaceAll(m, "/ubuntu/", "/ubuntu-ports/"))
			}
			return results
		}
		// Geo failed: fall through to registry/built-in.
	}

	// Prefer registry (config-loaded) mirrors when present
	if reg != nil {
		if d, ok := reg.GetByType(mode); ok && len(d.Mirrors) > 0 {
			for _, m := range d.Mirrors {
				mirrors = append(mirrors, GetFullMirrorURL(m))
			}
			if len(mirrors) > 0 {
				return mirrors
			}
		}
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
	if mirror.HTTP() {
		if strings.HasPrefix(mirror.URL, "http://") {
			return mirror.URL
		}
		return BuildHTTPURL(mirror.URL)
	}
	if mirror.HTTPS() {
		if strings.HasPrefix(mirror.URL, "https://") {
			return mirror.URL
		}
		return BuildHTTPSURL(mirror.URL)
	}
	return BuildHTTPSURL(mirror.URL)
}

// normalizeAliasURL ensures the alias value is a full URL (adds https:// if missing)
func normalizeAliasURL(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return BuildHTTPSURL(raw)
}

// GetMirrorURLByAliases resolves alias to a fully qualified mirror URL.
// When reg is non-nil, config-loaded aliases are checked first.
// Returns "" when the alias cannot be resolved.
func GetMirrorURLByAliases(reg *distro.Registry, osType int, alias string) string {
	// Prefer registry (config-loaded) aliases when present
	if reg != nil {
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

// GetPredefinedConfiguration returns (benchmarkURL, hostPattern) for
// the given proxy mode. Registry-loaded entries override built-ins
// when reg is non-nil.
func GetPredefinedConfiguration(reg *distro.Registry, proxyMode int) (string, *regexp.Regexp) {
	if reg != nil {
		if d, ok := reg.GetByType(proxyMode); ok && d.URLPattern != nil {
			return d.BenchmarkURL, d.URLPattern
		}
	}
	if b, ok := builtinByMode[proxyMode]; ok {
		return b.benchmarkURL, b.hostPattern
	}
	return "", nil
}
