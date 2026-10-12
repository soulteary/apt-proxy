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

package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestProductionInstallerFilesRewriteAndCache(t *testing.T) {
	const debianRoot = "dists/trixie/main/installer-amd64/current/images/"
	artifacts := []string{"netboot/pxelinux.cfg/default", "netboot/debian-installer/amd64/pxelinux.0", "netboot/debian-installer/amd64/pxelinux.cfg/default", "netboot/debian-installer/amd64/bootnetx64.efi", "netboot/debian-installer/amd64/grubx64.efi", "netboot/ubuntu-installer/arm64/bootnetaa64.efi", "netboot/gtk/debian-installer/amd64/grub/grub.cfg", "netboot/debian-installer/amd64/grub/font.pf2", "netboot/debian-installer/amd64/grub/x86_64-efi/normal.mod", "netboot/debian-installer/amd64/grub/x86_64-efi/command.lst", "netboot/debian-installer/amd64/boot-screens/vesamenu.c32", "netboot/debian-installer/amd64/boot-screens/menu.cfg", "netboot/debian-installer/amd64/boot-screens/f10.txt", "netboot/debian-installer/amd64/boot-screens/splash.png", "SHA256SUMS", "MANIFEST.udebs", "netboot/debian-installer/amd64/linux", "netboot/debian-installer/amd64/initrd.gz", "netboot/ubuntu-installer/amd64/linux", "netboot/ubuntu-installer/amd64/initrd.gz", "netboot/netboot.tar.gz", "netboot/mini.iso", "hd-media/boot.img.gz"}
	for _, distribution := range []struct {
		name, prefix string
		mode         int
	}{
		{"ubuntu", "/ubuntu/", distro.TypeUbuntu}, {"ubuntu-ports", "/ubuntu-ports/", distro.TypeUbuntuPorts}, {"debian", "/debian/", distro.TypeDebian},
	} {
		for _, yamlRules := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/yaml=%v", distribution.name, yamlRules), func(t *testing.T) {
				installerRoot := debianRoot
				if distribution.mode == distro.TypeUbuntu {
					installerRoot = "dists/focal/main/installer-amd64/current/legacy-images/"
				}
				var hits atomic.Int32
				var expectedPath string
				payload := []byte{0x00, 0x1f, 0x8b, 0xff, 0x78, 0x7a, 0x42}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					if r.URL.Path != expectedPath {
						t.Errorf("rewritten path=%s", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/octet-stream")
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					if r.URL.Path == distribution.prefix+installerRoot+"netboot/debian-installer/amd64/boot-screens/prompt.cfg" {
						w.Header().Set("Cache-Control", "no-store")
					}
					_, _ = w.Write(payload)
				}))
				defer upstream.Close()
				cfg := &config.Config{CacheDir: t.TempDir(), Mode: distribution.mode, Listen: "127.0.0.1:0"}
				switch distribution.mode {
				case distro.TypeUbuntu:
					cfg.Mirrors.Ubuntu = upstream.URL + distribution.prefix
				case distro.TypeUbuntuPorts:
					cfg.Mirrors.UbuntuPorts = upstream.URL + distribution.prefix
				case distro.TypeDebian:
					cfg.Mirrors.Debian = upstream.URL + distribution.prefix
				}
				if yamlRules {
					path, err := filepath.Abs("../../config/distributions.yaml")
					if err != nil {
						t.Fatal(err)
					}
					cfg.DistributionsConfigPath = path
				}
				srv, err := NewServer(cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = srv.shutdown() })
				request := func(path string, status int, cacheState string) {
					t.Helper()
					expectedPath = distribution.prefix + path
					resp, err := srv.app.Test(httptest.NewRequest("GET", distribution.prefix+path, nil))
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if resp.StatusCode != status {
						t.Fatalf("%s: status=%d body=%q", path, resp.StatusCode, body)
					}
					if status == 200 {
						if !bytes.Equal(body, payload) {
							t.Fatalf("%s: body bytes changed: %x", path, body)
						}
						if cacheState != "" && resp.Header.Get("X-Cache") != cacheState {
							t.Fatalf("%s: X-Cache=%s want=%s", path, resp.Header.Get("X-Cache"), cacheState)
						}
						if cacheState != "" && resp.Header.Get("Cache-Control") != "max-age=3600" {
							t.Fatalf("installer freshness=%s", resp.Header.Get("Cache-Control"))
						}
					}
				}
				for i, filename := range artifacts {
					path := installerRoot + filename
					before := hits.Load()
					request(path, 200, "MISS")
					deadline := time.Now().Add(2 * time.Second)
					for srv.cache.Stats().ItemCount < i+1 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					if srv.cache.Stats().ItemCount < i+1 {
						t.Fatalf("installer entry not stored: %s", filename)
					}
					request(path, 200, "HIT")
					if hits.Load() != before+1 {
						t.Fatalf("warm installer contacted upstream: %s", filename)
					}
				}
				before := hits.Load()
				for _, path := range []string{"netboot/pxelinux.cfg/default", "netboot/debian-installer/amd64/pxelinux.0", "netboot/debian-installer/amd64/pxelinux.cfg/default", "netboot/debian-installer/amd64/bootnetx64.efi", "netboot/debian-installer/amd64/grubx64.efi", "netboot/ubuntu-installer/arm64/bootnetaa64.efi", "netboot/gtk/debian-installer/amd64/grub/grub.cfg", "netboot/debian-installer/amd64/grub/font.pf2", "netboot/debian-installer/amd64/grub/x86_64-efi/normal.mod", "netboot/debian-installer/amd64/grub/x86_64-efi/command.lst", "netboot/debian-installer/amd64/boot-screens/vesamenu.c32", "netboot/debian-installer/amd64/boot-screens/menu.cfg", "netboot/debian-installer/amd64/boot-screens/f10.txt", "netboot/debian-installer/amd64/boot-screens/splash.png", "SHA256SUMS", installerRoot + "random.iso", installerRoot + "netboot/mini.iso.exe"} {
					request(path, 404, "")
				}
				if hits.Load() != before {
					t.Fatal("unsupported archive reached upstream")
				}
				request(installerRoot+"netboot/debian-installer/amd64/boot-screens/prompt.cfg", 200, "")
				request(installerRoot+"netboot/debian-installer/amd64/boot-screens/prompt.cfg", 200, "")
				if hits.Load() != before+2 {
					t.Fatal("upstream no-store installer response reused")
				}
				if srv.cache.Stats().ItemCount != len(artifacts) {
					t.Fatal("no-store installer response entered cache")
				}
			})
		}
	}
}
