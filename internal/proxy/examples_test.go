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
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// examples/custom-distros/distributions.yaml is meant to be copied and used as
// it stands, so it is exercised as shipped rather than as prose. A recipe that
// no longer routes is worse than no recipe: it sends people to debug their
// mirror or their client.
const customDistrosExample = "../../examples/custom-distros/distributions.yaml"

func exampleRegistry(t *testing.T) *distro.Registry {
	t.Helper()
	reg := distro.NewBuiltinRegistry()
	if err := reg.Reload(filepath.FromSlash(customDistrosExample)); err != nil {
		t.Fatalf("loading %s: %v", customDistrosExample, err)
	}
	return reg
}

func TestCustomDistrosExampleRegisters(t *testing.T) {
	reg := exampleRegistry(t)

	for id, wantType := range map[string]int{"deepin": 6, "armbian": 7, "archlinux": 10} {
		d, ok := reg.GetByID(id)
		if !ok {
			t.Errorf("%s is missing from the example", id)
			continue
		}
		if d.Type != wantType {
			t.Errorf("%s type = %d, want %d", id, d.Type, wantType)
		}
	}

	// The built-ins must survive alongside them.
	for _, id := range []string{"ubuntu", "debian", "centos", "alpine", "ubuntu-ports"} {
		if _, ok := reg.GetByID(id); !ok {
			t.Errorf("built-in %s was lost", id)
		}
	}
}

// Every request shape the example's README tells a client to make must route,
// including the package indexes that a missing catch-all rule would 404.
func TestCustomDistrosExampleRoutesClientRequests(t *testing.T) {
	reg := exampleRegistry(t)

	tests := []struct {
		name string
		host string
		path string
	}{
		{"deepin InRelease", "apt-proxy.example", "/deepin/dists/apricot/InRelease"},
		{"deepin Packages index", "apt-proxy.example", "/deepin/dists/apricot/main/binary-amd64/Packages.xz"},
		{"deepin by-hash", "apt-proxy.example", "/deepin/dists/apricot/main/by-hash/SHA256/deadbeef"},
		{"deepin package", "apt-proxy.example", "/deepin/pool/main/h/hello/hello_2.10_amd64.deb"},

		{"armbian at the domain root", "apt.armbian.com", "/dists/bookworm/InRelease"},
		{"armbian index at the domain root", "apt.armbian.com", "/dists/bookworm/main/binary-arm64/Packages.xz"},
		{"armbian via the path form", "apt-proxy.example", "/armbian/dists/bookworm/InRelease"},

		{"arch database", "apt-proxy.example", "/archlinux/core/os/x86_64/core.db"},
		{"arch database signature", "apt-proxy.example", "/archlinux/core/os/x86_64/core.db.sig"},
		{"arch files database", "apt-proxy.example", "/archlinux/extra/os/x86_64/extra.files"},
		{"arch package", "apt-proxy.example", "/archlinux/core/os/x86_64/bash-5.2-1-x86_64.pkg.tar.zst"},
		{"arch package signature", "apt-proxy.example", "/archlinux/core/os/x86_64/bash-5.2-1-x86_64.pkg.tar.zst.sig"},
		{"arch lastupdate", "apt-proxy.example", "/archlinux/lastupdate"},
	}

	ps, err := NewPackageStruct(Options{
		State:    newTestState(),
		Registry: reg,
		CacheDir: t.TempDir(),
		Mode:     distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.Host = tt.host
			if rule := ps.handleExternalURLs(r); rule == nil {
				t.Fatalf("%s %s did not match any distribution (would be a 404)", tt.host, tt.path)
			}
		})
	}
}

// Paths the example must not claim.
func TestCustomDistrosExampleDoesNotOverreach(t *testing.T) {
	reg := exampleRegistry(t)
	ps, err := NewPackageStruct(Options{
		State:    newTestState(),
		Registry: reg,
		CacheDir: t.TempDir(),
		Mode:     distro.TypeAllDistros,
	})
	if err != nil {
		t.Fatalf("NewPackageStruct: %v", err)
	}

	for _, tt := range []struct{ host, path string }{
		{"apt-proxy.example", "/not-a-distro/dists/x/InRelease"},
		{"apt.armbian.com.evil.test", "/dists/bookworm/InRelease"},
		{"apt-proxy.example", "/ppa.launchpad.net/user/ppa/archlinux/core.db"},
	} {
		r := httptest.NewRequest(http.MethodGet, tt.path, nil)
		r.Host = tt.host
		if rule := ps.handleExternalURLs(r); rule != nil {
			t.Errorf("%s %s matched but should 404", tt.host, tt.path)
		}
	}
}
