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

package distro

import "regexp"

// debStyleCachePatterns is the canonical set of cache rule patterns shared by
// Debian-family distributions (Ubuntu, Ubuntu Ports, Debian). They differ only
// in OS type, so we materialise them via newDebStyleRules to avoid copy/paste
// drift across the per-distro files.
var debStyleCachePatterns = []struct {
	pattern      *regexp.Regexp
	cacheControl string
}{
	{regexp.MustCompile(`deb$`), `max-age=100000`},
	{regexp.MustCompile(`udeb$`), `max-age=100000`},
	{regexp.MustCompile(`InRelease$`), `max-age=3600`},
	{regexp.MustCompile(`DiffIndex$`), `max-age=3600`},
	{regexp.MustCompile(`PackagesIndex$`), `max-age=3600`},
	{regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), `max-age=3600`},
	{regexp.MustCompile(`SourcesIndex$`), `max-age=3600`},
	{regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), `max-age=3600`},
	{regexp.MustCompile(`Release(\.gpg)?$`), `max-age=3600`},
	{regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), `max-age=3600`},
	{regexp.MustCompile(`\/by-hash\/`), `max-age=3600`},
}

// newDebStyleRules returns a fresh set of cache rules for the given OS type.
// Each call returns a new slice so callers can mutate freely without affecting
// the canonical template.
func newDebStyleRules(osType int) []Rule {
	rules := make([]Rule, len(debStyleCachePatterns))
	for i, p := range debStyleCachePatterns {
		rules[i] = Rule{
			OS:           osType,
			Pattern:      p.pattern,
			CacheControl: p.cacheControl,
			Rewrite:      true,
		}
	}
	return rules
}
