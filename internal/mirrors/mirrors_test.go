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

package mirrors

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestUbuntuPortsUsesRegistryMirrors(t *testing.T) {
	reg := distro.NewRegistry()
	want := []string{
		"https://ports-one.example.com/ubuntu-ports/",
		"https://ports-two.example.com/ubuntu-ports/",
	}
	if err := reg.Register(&distro.RegisteredDistribution{
		ID:         "ubuntu-ports",
		Name:       "Ubuntu Ports",
		Type:       distro.TypeUbuntuPorts,
		URLPattern: regexp.MustCompile(`/ubuntu-ports/`),
		Mirrors: []distro.URLWithAlias{
			{URL: want[0], Scheme: "https"},
			{URL: want[1], Scheme: "https"},
		},
	}); err != nil {
		t.Fatalf("register ubuntu ports: %v", err)
	}

	if got := GetGeoMirrorUrlsByMode(reg, distro.TypeUbuntuPorts); !reflect.DeepEqual(got, want) {
		t.Fatalf("configured Ubuntu Ports mirrors = %v, want %v", got, want)
	}
}

func TestUbuntuPortsUsesDedicatedBuiltins(t *testing.T) {
	want := builtinMirrorURLs(distro.BuiltinUbuntuPortsMirrors)
	if got := GetGeoMirrorUrlsByMode(nil, distro.TypeUbuntuPorts); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ubuntu Ports mirrors = %v, want dedicated built-ins %v", got, want)
	}
}

func TestGetUbuntuMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(nil, distro.TypeUbuntu, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/ubuntu/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(nil, distro.TypeUbuntu, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetDebianMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(nil, distro.TypeDebian, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/debian/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(nil, distro.TypeDebian, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetCentOSMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(nil, distro.TypeCentOS, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/centos/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(nil, distro.TypeCentOS, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetMirrorUrlsByGeo(t *testing.T) {
	mirrors := GetGeoMirrorUrlsByMode(nil, distro.TypeAllDistros)
	if len(mirrors) == 0 {
		t.Fatal("No mirrors found")
	}

	mirrors = GetGeoMirrorUrlsByMode(nil, distro.TypeDebian)
	if len(mirrors) != len(distro.BuiltinDebianMirrors) {
		t.Fatal("Get mirrors error")
	}

	mirrors = GetGeoMirrorUrlsByMode(nil, distro.TypeUbuntu)
	if len(mirrors) == 0 {
		t.Fatal("No mirrors found")
	}
}

func TestGetPredefinedConfiguration(t *testing.T) {
	res, pattern := GetPredefinedConfiguration(nil, distro.TypeUbuntu)
	if res != distro.UbuntuBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}
	if !pattern.MatchString("/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(nil, distro.TypeDebian)
	if res != distro.DebianBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/debian/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(nil, distro.TypeCentOS)
	if res != distro.CentosBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/centos/test/repomd.xml") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(nil, distro.TypeAlpine)
	if res != distro.AlpineBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/alpine/test/APKINDEX.tar.gz") {
		t.Fatal("Failed to verify domain name rules")
	}
}
