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

package proxy

import (
	"net"
	"regexp"
	"strings"
)

// Distribution URL patterns are deliberately unanchored (`/debian/(.+)$`) so
// the apt-cacher-ng-style host-prefixed form keeps working:
//
//	deb http://apt-proxy.example:3142/ftp.uni-kl.de/debian bookworm main
//
// On its own that is too generous. A third-party archive whose path merely
// *contains* a distribution segment matches the same pattern, and the request
// is then rewritten onto the distribution's own mirror. A deadsnakes PPA
// request
//
//	/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease
//
// came back as the main Ubuntu archive's index for that suite: a different
// repository's content, served with 200 and no warning. Third-party archives
// laid out as <host>/<path>/ubuntu/... (Docker's download.docker.com/linux/
// ubuntu, most vendor repos) hit the same trap.
//
// So the prefix has to look like the apt-cacher-ng form and nothing else:
// empty, or exactly one host-shaped segment. Anything deeper is not a mirror
// host, so it is reported as no match and the request 404s instead of being
// answered from the wrong archive.

// matchDistroPath matches path against a distribution's URL pattern, rejecting
// matches that sit behind a path prefix which is not a mirror host. It returns
// the submatches exactly as regexp.FindStringSubmatch would, or nil for no
// match.
func matchDistroPath(pattern *regexp.Regexp, path string) []string {
	loc := pattern.FindStringSubmatchIndex(path)
	if loc == nil || !acceptableHostPrefix(path[:loc[0]]) {
		return nil
	}
	groups := make([]string, 0, len(loc)/2)
	for i := 0; i < len(loc); i += 2 {
		// A non-participating optional group reports -1, which
		// FindStringSubmatch surfaces as an empty string.
		if loc[i] < 0 {
			groups = append(groups, "")
			continue
		}
		groups = append(groups, path[loc[i]:loc[i+1]])
	}
	return groups
}

// matchesDistroPath is the boolean form of matchDistroPath, for callers that
// only need to know whether the distribution claims this path.
func matchesDistroPath(pattern *regexp.Regexp, path string) bool {
	loc := pattern.FindStringIndex(path)
	return loc != nil && acceptableHostPrefix(path[:loc[0]])
}

// acceptableHostPrefix reports whether prefix -- everything in the request
// path ahead of the matched distribution segment -- is a host prefix
// apt-proxy honours.
func acceptableHostPrefix(prefix string) bool {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		// Native form: /ubuntu/dists/... with nothing in front.
		return true
	}
	if strings.ContainsRune(prefix, '/') {
		// More than one segment, so not a bare mirror host.
		return false
	}
	return looksLikeMirrorHost(prefix)
}

// looksLikeMirrorHost reports whether seg could be a mirror's hostname. An
// archive host is a registered DNS name and therefore carries a dot; IP
// literals and "localhost" are accepted as the conventional exceptions.
// Keeping this strict is what separates /ftp.uni-kl.de/debian/... (a host)
// from /deadsnakes/ppa/ubuntu/... (not a host).
func looksLikeMirrorHost(seg string) bool {
	host := seg
	if h, _, err := net.SplitHostPort(seg); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return true
	}
	dot := strings.IndexByte(host, '.')
	return dot > 0 && dot < len(host)-1
}
