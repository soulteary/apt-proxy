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

func TestDebStyleAppStreamCoverage(t *testing.T) {
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
			t.Fatalf("missing distribution: %s", id)
		}
		for source, rules := range map[string][]Rule{"builtin": newDebStyleRules(dist.Type), "yaml": dist.CacheRules} {
			t.Run(id+"/"+source, func(t *testing.T) {
				for _, root := range []string{"dists/trixie/main/dep11/", "/ubuntu/dists/noble-updates/universe/dep11/", "/debian/dists/trixie/non-free-firmware/dep11/"} {
					for _, filename := range []string{"Components-amd64.yml.xz", "Components-arm64.yml.gz", "Components-ppc64el.yml.xz", "CID-Index-amd64.json.gz", "CID-Index-arm64.json.gz", "icons-48x48.tar.gz", "icons-48x48@2.tar.gz", "icons-64x64.tar.gz", "icons-64x64@2.tar.gz", "icons-128x128.tar.gz", "icons-128x128@2.tar.gz"} {
						path := root + filename
						matched := false
						for _, rule := range rules {
							if rule.Pattern.MatchString(path) {
								matched = true
								if rule.OS != dist.Type || rule.CacheControl != "max-age=3600" || !rule.Rewrite {
									t.Errorf("incorrect metadata policy: %s", path)
								}
							}
						}
						if !matched {
							t.Errorf("missing metadata path: %s", path)
						}
					}
				}
				for _, path := range []string{
					"/debian/Components-amd64.yml.xz", "dists/trixie/main/Components-amd64.yml.xz",
					"pool/main/dep11/Components-amd64.yml.xz", "dists/trixie/main/dep11/nested/Components-amd64.yml.xz",
					"dists/trixie/main/dep11/Components-amd64.yml.xz.exe", "dists/trixie/main/dep11/Components-amd64.yml.xz/extra",
					"dists/trixie/main/dep11/CID-Index-amd64.json.gz.sig", "dists/trixie/main/dep11/random.json.gz",
					"dists/trixie/main/dep11/icons-999x999.tar.gz", "dists/trixie/main/dep11/icons-64x64@3.tar.gz",
					"dists/trixie/main/dep11/icons-64x64.tar.gz.exe", "dists/trixie/main/dep11/other.tar.gz",
				} {
					for _, rule := range rules {
						if rule.Pattern.MatchString(path) {
							t.Errorf("unexpected metadata match: %s", path)
						}
					}
				}
			})
		}
	}
}
