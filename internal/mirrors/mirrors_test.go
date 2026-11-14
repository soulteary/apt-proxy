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

func TestUbuntuBuiltinRegistryStillUsesGeoDiscovery(t *testing.T) {
	want := []string{"https://geo.example.com/ubuntu/"}
	original := ubuntuGeoMirrorLookup
	ubuntuGeoMirrorLookup = func() ([]string, error) {
		return want, nil
	}
	t.Cleanup(func() { ubuntuGeoMirrorLookup = original })

	reg := distro.NewBuiltinRegistry()
	if got := GetGeoMirrorUrlsByMode(reg, distro.TypeUbuntu); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ubuntu mirrors = %v, want geo-discovered mirrors %v", got, want)
	}
}

func TestUbuntuConfiguredRegistryOverridesGeoDiscovery(t *testing.T) {
	want := []string{"https://configured.example.com/ubuntu/"}
	called := false
	original := ubuntuGeoMirrorLookup
	ubuntuGeoMirrorLookup = func() ([]string, error) {
		called = true
		return []string{"https://geo.example.com/ubuntu/"}, nil
	}
	t.Cleanup(func() { ubuntuGeoMirrorLookup = original })

	reg := distro.NewRegistry()
	if err := reg.Register(&distro.RegisteredDistribution{
		ID:   "ubuntu",
		Name: "Ubuntu",
		Type: distro.TypeUbuntu,
		Mirrors: []distro.URLWithAlias{
			{URL: want[0], Scheme: "https"},
		},
	}); err != nil {
		t.Fatalf("register Ubuntu: %v", err)
	}

	if got := GetGeoMirrorUrlsByMode(reg, distro.TypeUbuntu); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ubuntu mirrors = %v, want configured mirrors %v", got, want)
	}
	if called {
		t.Fatal("geo discovery called despite configured Ubuntu mirrors")
	}
}

func TestGetUbuntuMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(nil, distro.TypeUbuntu, "cn:tsinghua")
	if alias != "https://mirrors.tuna.tsinghua.edu.cn/ubuntu/" {
		t.Fatalf("cn:tsinghua resolved to %q, want the https mirror", alias)
	}

	alias = GetMirrorURLByAliases(nil, distro.TypeUbuntu, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetDebianMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(nil, distro.TypeDebian, "cn:tsinghua")
	if alias != "https://mirrors.tuna.tsinghua.edu.cn/debian/" {
		t.Fatalf("cn:tsinghua resolved to %q, want the https mirror", alias)
	}

	alias = GetMirrorURLByAliases(nil, distro.TypeDebian, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetCentOSMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(nil, distro.TypeCentOS, "cn:tsinghua")
	if alias != "https://mirrors.tuna.tsinghua.edu.cn/centos/" {
		t.Fatalf("cn:tsinghua resolved to %q, want the https mirror", alias)
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

// customListedDistro registers a distributions.yaml-style entry of the given
// type with one listed mirror and, optionally, an official address.
func customListedDistro(t *testing.T, reg *distro.Registry, id string, distType int, official string) {
	t.Helper()
	if err := reg.LoadFromConfig(&distro.DistributionConfig{
		ID: id, Name: id, Type: distType,
		URLPattern: "/" + id + "/(.+)$", BenchmarkURL: "probe", OfficialURL: official,
		Mirrors: distro.MirrorListConfig{Official: []string{"listed.example.com/" + id + "/"}},
	}); err != nil {
		t.Fatalf("LoadFromConfig(%s): %v", id, err)
	}
}

func TestGetOfficialMirrorURL(t *testing.T) {
	builtin := map[int]string{
		distro.TypeUbuntu:      "http://archive.ubuntu.com/ubuntu/",
		distro.TypeUbuntuPorts: "http://ports.ubuntu.com/ubuntu-ports/",
		distro.TypeDebian:      "https://deb.debian.org/debian/",
		distro.TypeCentOS:      "",
		distro.TypeAlpine:      "https://dl-cdn.alpinelinux.org/alpine/",
		// Not a distribution: the all-distributions mode and an unknown type.
		distro.TypeAllDistros: "",
		999:                   "",
	}
	for _, reg := range []*distro.Registry{nil, distro.NewRegistry(), distro.NewBuiltinRegistry()} {
		for mode, want := range builtin {
			if got := GetOfficialMirrorURL(reg, mode); got != want {
				t.Errorf("GetOfficialMirrorURL(%d) = %q, want %q", mode, got, want)
			}
		}
	}

	// A registry entry decides for its type, a built-in one included.
	reg := distro.NewBuiltinRegistry()
	customListedDistro(t, reg, "debian", distro.TypeDebian, "")
	customListedDistro(t, reg, "alpine", distro.TypeAlpine, "repo.example.com/alpine/")
	customListedDistro(t, reg, "deepin", 42, "http://packages.example.com/deepin/")
	customListedDistro(t, reg, "armbian", 43, "")
	for mode, want := range map[int]string{
		distro.TypeDebian: "",
		distro.TypeAlpine: "https://repo.example.com/alpine/",
		42:                "http://packages.example.com/deepin/",
		43:                "",
		distro.TypeUbuntu: "http://archive.ubuntu.com/ubuntu/",
	} {
		if got := GetOfficialMirrorURL(reg, mode); got != want {
			t.Errorf("configured GetOfficialMirrorURL(%d) = %q, want %q", mode, got, want)
		}
	}
}

// The listed mirrors stand in for an official address, so they must come from
// the distribution itself and must not involve a request.
func TestGetListedMirrorUrlsByMode(t *testing.T) {
	original := ubuntuGeoMirrorLookup
	ubuntuGeoMirrorLookup = func() ([]string, error) {
		t.Error("listing mirrors asked the geo API")
		return nil, nil
	}
	t.Cleanup(func() { ubuntuGeoMirrorLookup = original })

	for _, reg := range []*distro.Registry{nil, distro.NewRegistry(), distro.NewBuiltinRegistry()} {
		for mode, list := range map[int][]distro.URLWithAlias{
			distro.TypeUbuntu:      distro.BuiltinUbuntuMirrors,
			distro.TypeUbuntuPorts: distro.BuiltinUbuntuPortsMirrors,
			distro.TypeDebian:      distro.BuiltinDebianMirrors,
			distro.TypeCentOS:      distro.BuiltinCentosMirrors,
			distro.TypeAlpine:      distro.BuiltinAlpineMirrors,
		} {
			if got, want := GetListedMirrorUrlsByMode(reg, mode), builtinMirrorURLs(list); !reflect.DeepEqual(got, want) {
				t.Errorf("GetListedMirrorUrlsByMode(%d) = %v, want the built-in list %v", mode, got, want)
			}
		}
		// The candidate lookup answers these with every built-in mirror.
		for _, mode := range []int{distro.TypeAllDistros, 999} {
			if got := GetListedMirrorUrlsByMode(reg, mode); got != nil {
				t.Errorf("GetListedMirrorUrlsByMode(%d) = %v, want no mirrors", mode, got)
			}
		}
	}
	if got := GetListedMirrorUrlsByMode(nil, distro.TypeCentOS)[0]; got != "https://mirrors.bfsu.edu.cn/centos/" {
		t.Errorf("first listed CentOS mirror = %q", got)
	}

	reg := distro.NewBuiltinRegistry()
	customListedDistro(t, reg, "ubuntu", distro.TypeUbuntu, "")
	customListedDistro(t, reg, "deepin", 42, "")
	if err := reg.LoadFromConfig(&distro.DistributionConfig{ID: "empty", Name: "Empty", Type: 43, URLPattern: "/empty/(.+)$", BenchmarkURL: "probe"}); err != nil {
		t.Fatal(err)
	}
	for mode, want := range map[int][]string{
		distro.TypeUbuntu: {"https://listed.example.com/ubuntu/"},
		42:                {"https://listed.example.com/deepin/"},
		43:                nil,
	} {
		if got := GetListedMirrorUrlsByMode(reg, mode); !reflect.DeepEqual(got, want) {
			t.Errorf("configured GetListedMirrorUrlsByMode(%d) = %v, want %v", mode, got, want)
		}
	}
}

// The official addresses head the built-in candidate lists without an alias,
// so the aliases that existed before still resolve to the mirrors they named.
func TestOfficialMirrorsAreCandidatesWithoutAliases(t *testing.T) {
	for mode, want := range map[int]string{
		distro.TypeUbuntuPorts: "http://ports.ubuntu.com/ubuntu-ports/",
		distro.TypeDebian:      "https://deb.debian.org/debian/",
		distro.TypeAlpine:      "https://dl-cdn.alpinelinux.org/alpine/",
	} {
		if got := GetGeoMirrorUrlsByMode(nil, mode); len(got) == 0 || got[0] != want {
			t.Errorf("candidates of type %d do not start with the official address %s", mode, want)
		}
	}
	for _, tc := range []struct {
		mode        int
		alias, want string
	}{
		{distro.TypeUbuntu, "cn:ubuntu", "http://cn.archive.ubuntu.com/ubuntu/"},
		{distro.TypeDebian, "cn:debian", "http://ftp.cn.debian.org/debian/"},
		// An entry without an alias is not selected by an empty name.
		{distro.TypeUbuntu, "", ""},
		{distro.TypeDebian, "", ""},
	} {
		if got := GetMirrorURLByAliases(nil, tc.mode, tc.alias); got != tc.want {
			t.Errorf("alias %q of type %d resolved to %q, want %q", tc.alias, tc.mode, got, tc.want)
		}
		if got := GetMirrorURLByAliases(distro.NewBuiltinRegistry(), tc.mode, tc.alias); got != tc.want {
			t.Errorf("registry alias %q of type %d resolved to %q, want %q", tc.alias, tc.mode, got, tc.want)
		}
	}
}
