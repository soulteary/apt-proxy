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

// Package distro provides distribution-specific definitions and caching rules
// for apt-proxy. This package contains constants, types, and configurations
// for supported Linux distributions (Ubuntu, Debian, CentOS, Alpine).
package distro

import (
	"fmt"
	"regexp"
	"strings"
)

// Distribution name constants
const (
	DistroAll         string = "all"
	DistroUbuntu      string = "ubuntu"
	DistroUbuntuPorts string = "ubuntu-ports"
	DistroDebian      string = "debian"
	DistroCentOS      string = "centos"
	DistroAlpine      string = "alpine"
)

// Distribution type constants
const (
	TypeAllDistros  int = 0
	TypeUbuntu      int = 1
	TypeUbuntuPorts int = 2
	TypeDebian      int = 3
	TypeCentOS      int = 4
	TypeAlpine      int = 5
)

// DistributionName returns the distribution ID string for the given type.
// Returns empty string for unknown types.
func DistributionName(distType int) string {
	switch distType {
	case TypeUbuntu:
		return DistroUbuntu
	case TypeUbuntuPorts:
		return DistroUbuntuPorts
	case TypeDebian:
		return DistroDebian
	case TypeCentOS:
		return DistroCentOS
	case TypeAlpine:
		return DistroAlpine
	default:
		return ""
	}
}

// Rule defines a caching rule for package files
type Rule struct {
	OS           int
	Pattern      *regexp.Regexp
	CacheControl string
	Rewrite      bool
}

func (r *Rule) String() string {
	return fmt.Sprintf("%s Cache-Control=%s Rewrite=%#v",
		r.Pattern.String(), r.CacheControl, r.Rewrite)
}

// URLWithAlias represents a mirror URL with its alias and metadata.
// Scheme is "http", "https", or "" (unknown / let downstream pick the default).
type URLWithAlias struct {
	URL       string
	Alias     string
	Scheme    string
	Official  bool
	Bandwidth int64
}

// HTTP reports whether the URL was registered as an HTTP-only mirror. It is a
// thin convenience over Scheme retained so existing callers don't need to be
// updated. Prefer reading Scheme directly in new code.
func (u URLWithAlias) HTTP() bool { return u.Scheme == "http" }

// HTTPS reports whether the URL was registered as an HTTPS mirror. See HTTP
// for the migration note.
func (u URLWithAlias) HTTPS() bool { return u.Scheme == "https" }

// GenerateAliasFromURL generates an alias from a URL
func GenerateAliasFromURL(url string) string {
	pureHost := urlSchemeAndPathRegex.ReplaceAllString(url, "")
	tldRemoved := tldRemovalRegex.ReplaceAllString(pureHost, "")
	group := strings.Split(tldRemoved, ".")
	alias := group[len(group)-1]
	return "cn:" + alias
}

// GenerateBuiltinMirrorItem creates a URLWithAlias from a URL.
func GenerateBuiltinMirrorItem(url string, official bool) URLWithAlias {
	var mirror URLWithAlias
	mirror.Official = official
	mirror.Alias = GenerateAliasFromURL(url)

	switch {
	case strings.HasPrefix(url, "http://"):
		mirror.Scheme = "http"
	case strings.HasPrefix(url, "https://"):
		mirror.Scheme = "https"
	}
	mirror.URL = url
	mirror.Bandwidth = 0
	return mirror
}

// GenerateBuildInMirorItem is the original misspelt name kept for callers
// that haven't migrated. New code should use GenerateBuiltinMirrorItem.
//
// Deprecated: use GenerateBuiltinMirrorItem.
func GenerateBuildInMirorItem(url string, official bool) URLWithAlias {
	return GenerateBuiltinMirrorItem(url, official)
}

var (
	urlSchemeAndPathRegex = regexp.MustCompile(`^https?://|\/.*`)
	tldRemovalRegex       = regexp.MustCompile(`\.edu\.cn$|\.cn$|\.com$|\.net$|\.net\.cn$|\.org$|\.org\.cn$`)
)

// GenerateBuiltinList generates a list of mirror URLs with aliases.
func GenerateBuiltinList(officialList []string, customList []string) (mirrors []URLWithAlias) {
	mirrors = appendBuiltins(mirrors, officialList, true)
	mirrors = appendBuiltins(mirrors, customList, false)
	return mirrors
}

// GenerateBuildInList is the original misspelt name kept for callers that
// haven't migrated.
//
// Deprecated: use GenerateBuiltinList.
func GenerateBuildInList(officialList []string, customList []string) []URLWithAlias {
	return GenerateBuiltinList(officialList, customList)
}

func appendBuiltins(mirrors []URLWithAlias, list []string, official bool) []URLWithAlias {
	for _, url := range list {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			mirrors = append(mirrors, GenerateBuiltinMirrorItem("http://"+url, official))
			mirrors = append(mirrors, GenerateBuiltinMirrorItem("https://"+url, official))
			continue
		}
		mirrors = append(mirrors, GenerateBuiltinMirrorItem(url, official))
	}
	return mirrors
}
