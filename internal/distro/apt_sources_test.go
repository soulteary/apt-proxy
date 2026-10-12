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

import "testing"

func TestDebStyleSourceCoverage(t *testing.T) {
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
	for _, id := range []string{DistroUbuntu, DistroUbuntuPorts, DistroDebian} {
		dist, ok := reg.GetByID(id)
		if !ok {
			t.Fatalf("missing %s", id)
		}
		for name, rules := range map[string][]Rule{"builtin": newDebStyleRules(dist.Type), "yaml": dist.CacheRules} {
			t.Run(id+"/"+name, func(t *testing.T) {
				positives := []string{"demo_1.2-3.dsc", "demo_1.2.orig.tar.gz", "demo_1.2.orig-docs-v2.tar.xz", "demo_1.2-3.debian.tar.xz", "demo_1.2.tar.gz", "demo_1.2-3.diff.gz", "demo_1.2.orig.tar.gz.asc", "demo_1.2.orig-Docs2.tar.xz.asc", "demo_1.2+dfsg~rc1-2.dsc", "demo_1.2.tar.bz2", "demo_1.2.tar.lzma"}
				for _, compression := range []string{"gz", "bz2", "lzma", "xz"} {
					positives = append(positives, "demo_1.2.orig.tar."+compression, "demo_1.2-3.debian.tar."+compression, "demo_1.2.tar."+compression)
				}
				for _, prefix := range []string{"pool/main/d/demo/", "/ubuntu/pool/universe/d/demo/", "/ubuntu-ports/pool/main/d/demo/", "/debian/pool/non-free-firmware/d/demo/", "/debian-security/pool/updates/main/d/demo/"} {
					for _, filename := range positives {
						path := prefix + filename
						matched := false
						for _, rule := range rules {
							if rule.Pattern.MatchString(path) {
								matched = true
								if !rule.Rewrite || rule.OS != dist.Type || rule.CacheControl != "max-age=100000" {
									t.Errorf("wrong source policy: %s %+v", path, rule)
								}
							}
						}
						if !matched {
							t.Errorf("source file not matched: %s", path)
						}
					}
				}
				for _, path := range []string{"/ubuntu/archives/demo_1.2.orig.tar.gz", "/ubuntu/dists/stable/demo_1.2.dsc", "/ubuntu/pool/main/d/demo/readme.tar.gz", "/ubuntu/pool/main/d/demo/demo_1.2.orig.tar.gz.exe", "/ubuntu/pool/main/d/demo/demo_1.2.orig.tar.gz.sig", "/ubuntu/pool/main/d/demo/demo_1.2.debian.tar.xz.asc", "/ubuntu/pool/main/d/demo/demo_1.2.orig-extra_component.tar.xz", "/ubuntu/pool/main/d/demo/demo_1.2.diff.xz", "/ubuntu/pool/main/d/demo/demo_1.2.tar.zst", "/ubuntu/pool/main/d/demo/demo_1.2.dsc/extra", "/ubuntu/pool/main/d/demo/demo_1.2.orig.tar.gz/extra"} {
					for _, rule := range rules {
						if rule.Pattern.MatchString(path) {
							t.Errorf("unexpected source match: %s", path)
						}
					}
				}
			})
		}
	}
}
