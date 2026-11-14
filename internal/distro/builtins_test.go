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
	"reflect"
	"strings"
	"testing"
)

// config/distributions.yaml repeats the benchmark path of every built-in
// distribution and overrides it wherever the file is loaded, so a copy that
// drifts from the constant breaks mirror election there.
func TestShippedConfigBenchmarkURLsMatchBuiltins(t *testing.T) {
	cfg, err := NewLoader("../../config/distributions.yaml").Load()
	if err != nil {
		t.Fatal(err)
	}
	shipped := make(map[string]string, len(cfg.Distributions))
	for _, dist := range cfg.Distributions {
		shipped[dist.ID] = dist.BenchmarkURL
	}
	for id, want := range map[string]string{
		DistroUbuntu:      UbuntuBenchmarkURL,
		DistroUbuntuPorts: UbuntuPortsBenchmarkURL,
		DistroDebian:      DebianBenchmarkURL,
		DistroCentOS:      CentosBenchmarkURL,
		DistroAlpine:      AlpineBenchmarkURL,
	} {
		got, ok := shipped[id]
		if !ok {
			t.Errorf("%s is missing from config/distributions.yaml", id)
			continue
		}
		if got != want {
			t.Errorf("%s: benchmark_url = %q, want the built-in %q", id, got, want)
		}
	}
}

// A release codename in a benchmark path stops existing on mirrors once that
// release is archived; the suite aliases do not.
func TestBuiltinBenchmarkURLsUseSuiteAliases(t *testing.T) {
	for id, want := range map[string]string{
		DistroUbuntu:      "dists/devel/Release",
		DistroUbuntuPorts: "dists/devel/Release",
		DistroDebian:      "dists/stable/Release",
	} {
		dist, ok := NewBuiltinRegistry().GetByID(id)
		if !ok {
			t.Fatalf("missing built-in %s", id)
		}
		if dist.BenchmarkURL != want {
			t.Errorf("%s: BenchmarkURL = %q, want %q", id, dist.BenchmarkURL, want)
		}
	}
}

// Without an official address, the first entry of a list is the default mirror
// while an election is pending, and an alias resolves to the first entry
// carrying it. Both should land on https when the mirror is listed without a
// scheme.
func TestBuiltinListOffersHTTPSFirst(t *testing.T) {
	list := GenerateBuiltinList(
		[]string{"mirror.example/ubuntu/", "http://plain.example/ubuntu/"},
		[]string{"custom.example/ubuntu/"},
	)
	var got []string
	for _, mirror := range list {
		got = append(got, mirror.URL)
	}
	want := []string{
		"https://mirror.example/ubuntu/",
		"http://mirror.example/ubuntu/",
		"http://plain.example/ubuntu/",
		"https://custom.example/ubuntu/",
		"http://custom.example/ubuntu/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GenerateBuiltinList() = %v, want %v", got, want)
	}
}

// Not every Ubuntu mirror carries the ports archive. Hosts known to answer 404
// for it, or to redirect to a decommission notice, must not be offered as
// candidates.
func TestBuiltinListsOmitHostsWithoutTheArchive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mirrors []URLWithAlias
		hosts   []string
	}{
		{"ubuntu", BuiltinUbuntuMirrors, []string{"mirrors.dgut.edu.cn"}},
		{"ubuntu-ports", BuiltinUbuntuPortsMirrors, []string{
			"mirrors.cn99.com", "mirrors.sohu.com", "ftp.sjtu.edu.cn",
			"mirrors.xjtu.edu.cn", "cn.archive.ubuntu.com", "mirrors.dgut.edu.cn",
		}},
	} {
		if len(tc.mirrors) == 0 {
			t.Errorf("%s: built-in list is empty", tc.name)
		}
		for _, mirror := range tc.mirrors {
			for _, host := range tc.hosts {
				if strings.Contains(mirror.URL, "://"+host+"/") {
					t.Errorf("%s: built-in list still offers %s", tc.name, mirror.URL)
				}
			}
		}
	}
}
