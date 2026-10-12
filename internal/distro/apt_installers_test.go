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

func TestDebStyleInstallerCoverage(t *testing.T) {
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
				for _, root := range []string{"/ubuntu/dists/focal/main/installer-amd64/current/legacy-images/", "dists/trixie/main/installer-amd64/current/images/", "/debian/dists/trixie/main/installer-amd64/20250901+deb13u1/images/", "/ubuntu/dists/jammy-updates/main/installer-amd64/current/images/", "/ubuntu-ports/dists/jammy/main/installer-arm64/current/images/"} {
					for _, suffix := range []string{"MD5SUMS", "SHA256SUMS", "MANIFEST", "MANIFEST.udebs", "udeb.list", "netboot/netboot.tar.gz", "netboot/mini.iso", "netboot/pxelinux.0", "netboot/debian-installer/amd64/linux", "netboot/debian-installer/amd64/initrd.gz", "netboot/ubuntu-installer/arm64/linux", "netboot/ubuntu-installer/arm64/initrd.gz", "netboot/gtk/debian-installer/amd64/linux", "hd-media/vmlinuz", "hd-media/initrd.gz", "hd-media/boot.img.gz", "hd-media/gtk/initrd.gz", "cdrom/vmlinuz", "cdrom/initrd.gz", "cdrom/gtk/vmlinuz"} {
						path := root + suffix
						matched := false
						for _, rule := range rules {
							if rule.Pattern.MatchString(path) {
								matched = true
								if !rule.Rewrite || rule.OS != dist.Type || rule.CacheControl != "max-age=3600" {
									t.Errorf("wrong installer policy: %s", path)
								}
							}
						}
						if !matched {
							t.Errorf("installer path missing: %s", path)
						}
					}
				}
				for _, path := range []string{"/ubuntu/dists/focal/main/installer-amd64/current/other-images/MD5SUMS", "/ubuntu/dists/focal/main/installer-amd64/current/legacy-images/random.iso", "/ubuntu/dists/focal/main/installer-amd64/current/legacy-images/SHA256SUMS.exe", "/debian/MD5SUMS", "/debian/pool/main/SHA256SUMS", "/debian/dists/trixie/main/images/netboot/netboot.tar.gz", "/debian/dists/trixie/main/installer-amd64/current/netboot/linux", "/debian/dists/trixie/main/installer-amd64/current/images/random.tar.gz", "/debian/dists/trixie/main/installer-amd64/current/images/MD5SUMS.exe", "/debian/dists/trixie/main/installer-amd64/current/images/SHA256SUMS.sign", "/debian/dists/trixie/main/installer-amd64/current/images/netboot/mini.iso.exe", "/debian/dists/trixie/main/installer-amd64/current/images/netboot/netboot.tar.gz.exe", "/debian/dists/trixie/main/installer-amd64/current/images/netboot/debian-installer/amd64/linux/extra", "/debian/dists/trixie/main/installer-amd64/current/images/netboot/arbitrary-installer/amd64/linux", "/debian/dists/trixie/main/installer-amd64/current/images/cdrom/boot.img.gz"} {
					for _, rule := range rules {
						if rule.Pattern.MatchString(path) {
							t.Errorf("unexpected installer match: %s", path)
						}
					}
				}
			})
		}
	}
}
