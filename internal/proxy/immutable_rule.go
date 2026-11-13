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
	"net/http"
	"strings"

	"github.com/soulteary/apt-proxy/internal/distro"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// immutableRule reports whether the rule's Cache-Control carries the immutable
// directive: the matched URL never serves different content.
func immutableRule(rule *distro.Rule) bool {
	if rule == nil {
		return false
	}
	cc, err := httpcache.ParseCacheControl(rule.CacheControl)
	return err == nil && cc.Has("immutable")
}

// dropZeroMaxAge removes max-age=0 from the request's Cache-Control. APT sends
// it with every index request, by-hash paths included, and the cache treats a
// request that carries it as not cacheable. An explicit no-cache, in
// Cache-Control or Pragma, is left in place: it is how a client asks for a
// copy from the origin, for example with Acquire::http::No-Cache. Other
// directives keep their spelling and order; a field is rewritten only when
// something is removed from it.
func dropZeroMaxAge(h http.Header) {
	for name, lines := range h {
		if !strings.EqualFold(name, "Cache-Control") {
			continue
		}
		var kept []string
		removed := false
		for _, line := range lines {
			var directives []string
			for _, directive := range splitDirectives(line) {
				directive = strings.TrimSpace(directive)
				switch {
				case zeroMaxAge(directive):
					removed = true
				case directive != "":
					directives = append(directives, directive)
				}
			}
			if len(directives) > 0 {
				kept = append(kept, strings.Join(directives, ", "))
			}
		}
		if !removed {
			continue
		}
		if len(kept) == 0 {
			delete(h, name)
		} else {
			h[name] = kept
		}
	}
}

// zeroMaxAge reports whether a single directive is max-age with a zero value.
func zeroMaxAge(directive string) bool {
	name, value, _ := strings.Cut(directive, "=")
	if !strings.EqualFold(strings.TrimSpace(name), "max-age") {
		return false
	}
	value = strings.Trim(strings.TrimSpace(value), `"`)
	return value != "" && strings.Trim(value, "0") == ""
}

// splitDirectives splits a field value at the commas outside quoted strings.
func splitDirectives(line string) []string {
	var parts []string
	start, quoted := 0, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				parts = append(parts, line[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, line[start:])
}
