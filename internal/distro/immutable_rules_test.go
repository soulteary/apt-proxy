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

import (
	"strings"
	"testing"
)

func TestDebStyleImmutableArtifactPolicy(t *testing.T) {
	cfg, err := NewLoader("../../config/distributions.yaml").Load()
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	for i := range cfg.Distributions {
		if err := reg.LoadFromConfig(&cfg.Distributions[i]); err != nil {
			t.Fatal(err)
		}
	}
	policies := map[string][]string{
		"max-age=100000, immutable": {
			"/ubuntu/pool/main/d/demo/demo_1.2-3_amd64.deb",
			"pool/main/d/demo/demo-udeb_1.2-3_amd64.udeb",
			"/debian-security/pool/updates/main/d/demo/demo_1.2-3.dsc",
			"/ubuntu/pool/universe/d/demo/demo_1.2.orig.tar.xz",
			"dists/stable/main/binary-amd64/by-hash/SHA256/" + strings.Repeat("a", 64),
			// A digest can end in "deb" and reach the package rule first.
			"/ubuntu/dists/noble/main/dep11/by-hash/SHA256/" + strings.Repeat("a", 61) + "deb",
			"dists/stable/main/i18n/by-hash/MD5Sum/" + strings.Repeat("0", 32),
		},
		"max-age=3600": {
			"dists/stable/InRelease",
			"dists/stable/Release",
			"dists/stable/Release.gpg",
			"dists/stable/main/binary-amd64/Packages.xz",
			"dists/stable/main/source/Sources.xz",
			"dists/stable/main/i18n/Translation-en.xz",
			"dists/stable/main/Contents-amd64.gz",
			"dists/stable/main/binary-amd64/Packages.diff/Index",
			"dists/stable/main/dep11/Components-amd64.yml.xz",
			"dists/trixie/main/installer-amd64/current/images/SHA256SUMS",
		},
	}
	for _, id := range []string{DistroUbuntu, DistroUbuntuPorts, DistroDebian} {
		dist, ok := reg.GetByID(id)
		if !ok {
			t.Fatalf("missing %s", id)
		}
		for source, rules := range map[string][]Rule{"builtin": newDebStyleRules(dist.Type), "yaml": dist.CacheRules} {
			t.Run(id+"/"+source, func(t *testing.T) {
				for want, paths := range policies {
					for _, path := range paths {
						matched := false
						for _, rule := range rules {
							if rule.Pattern.MatchString(path) {
								matched = true
								if !rule.Rewrite || rule.OS != dist.Type || rule.CacheControl != want {
									t.Errorf("%s: policy %q, want %q", path, rule.CacheControl, want)
								}
							}
						}
						if !matched {
							t.Errorf("no rule for %s", path)
						}
					}
				}
			})
		}
	}
	// RPM and APK repositories keep their lifetimes.
	for id, builtin := range map[string][]Rule{DistroCentOS: CentosDefaultCacheRules, DistroAlpine: AlpineDefaultCacheRules} {
		dist, ok := reg.GetByID(id)
		if !ok {
			t.Fatalf("missing %s", id)
		}
		for source, rules := range map[string][]Rule{"builtin": builtin, "yaml": dist.CacheRules} {
			for _, rule := range rules {
				if strings.Contains(strings.ToLower(rule.CacheControl), "immutable") {
					t.Errorf("%s/%s: unexpected immutable rule %s", id, source, rule.String())
				}
			}
		}
	}
}
