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
	"os"
	"strings"
	"testing"
)

// builtinOfficialURLs is the documented official address of every built-in
// distribution. CentOS has none.
var builtinOfficialURLs = map[string]string{
	DistroUbuntu:      "http://archive.ubuntu.com/ubuntu/",
	DistroUbuntuPorts: "http://ports.ubuntu.com/ubuntu-ports/",
	DistroDebian:      "https://deb.debian.org/debian/",
	DistroCentOS:      "",
	DistroAlpine:      "https://dl-cdn.alpinelinux.org/alpine/",
}

func TestBuiltinDistributionsCarryOfficialURL(t *testing.T) {
	reg := NewBuiltinRegistry()
	for id, want := range builtinOfficialURLs {
		dist, ok := reg.GetByID(id)
		if !ok {
			t.Fatalf("missing built-in %s", id)
		}
		if dist.OfficialURL != want {
			t.Errorf("%s: OfficialURL = %q, want %q", id, dist.OfficialURL, want)
		}
	}
}

// The official address has to be an election candidate, and it has to be at
// the head of the list to be probed before the election deadline when the
// other mirrors time out.
func TestBuiltinListsStartWithOfficialMirror(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mirrors []URLWithAlias
		want    []string
	}{
		{"ubuntu", BuiltinUbuntuMirrors, []string{"http://archive.ubuntu.com/ubuntu/"}},
		{"ubuntu-ports", BuiltinUbuntuPortsMirrors, []string{"http://ports.ubuntu.com/ubuntu-ports/"}},
		{"debian", BuiltinDebianMirrors, []string{"https://deb.debian.org/debian/", "http://deb.debian.org/debian/"}},
		{"alpine", BuiltinAlpineMirrors, []string{"https://dl-cdn.alpinelinux.org/alpine/", "http://dl-cdn.alpinelinux.org/alpine/"}},
	} {
		if len(tc.mirrors) <= len(tc.want) {
			t.Errorf("%s: list has %d entries, want the official address and the mirrors", tc.name, len(tc.mirrors))
			continue
		}
		for i, want := range tc.want {
			got := tc.mirrors[i]
			scheme, _, _ := strings.Cut(want, "://")
			if got.URL != want || got.Scheme != scheme || !got.Official {
				t.Errorf("%s: entry %d = %+v, want the official address %s", tc.name, i, got, want)
			}
			// An alias here would take over the cn:<name> alias of a listed
			// mirror: cn:ubuntu and cn:debian exist.
			if got.Alias != "" {
				t.Errorf("%s: official address %s carries the alias %q", tc.name, want, got.Alias)
			}
		}
		if next := tc.mirrors[len(tc.want)]; next.Alias == "" {
			t.Errorf("%s: listed mirror %s lost its alias", tc.name, next.URL)
		}
	}

	// CentOS has no official address, so its list is the mirrors alone.
	if first := BuiltinCentosMirrors[0]; first.URL != "https://mirrors.bfsu.edu.cn/centos/" || first.Alias == "" {
		t.Errorf("centos: first entry = %+v, want the first listed mirror", first)
	}
}

// config/distributions.yaml replaces the built-in entries wherever it is
// loaded, and an entry does not inherit the built-in official address. The
// file has to name the same addresses, and list them as mirrors for them to
// take part in the election.
func TestShippedConfigOfficialURLsMatchBuiltins(t *testing.T) {
	cfg, err := NewLoader("../../config/distributions.yaml").Load()
	if err != nil {
		t.Fatal(err)
	}
	shipped := make(map[string]DistributionConfig, len(cfg.Distributions))
	for _, dist := range cfg.Distributions {
		shipped[dist.ID] = dist
	}
	for id, want := range builtinOfficialURLs {
		dist, ok := shipped[id]
		if !ok {
			t.Errorf("%s is missing from config/distributions.yaml", id)
			continue
		}
		got, err := normalizeOfficialURL(dist.OfficialURL)
		if err != nil || got != want {
			t.Errorf("%s: official_url = %q (%v), want the built-in %q", id, dist.OfficialURL, err, want)
		}
		if want == "" {
			continue
		}
		listed := false
		for _, mirror := range append(append([]string(nil), dist.Mirrors.Official...), dist.Mirrors.Custom...) {
			// A listed mirror without a scheme is fetched over https.
			if !strings.Contains(mirror, "://") {
				mirror = "https://" + mirror
			}
			listed = listed || mirror == want
		}
		if !listed {
			t.Errorf("%s: official_url %s is not among the listed mirrors", id, want)
		}
	}
}

const officialURLDistributionYAML = `distributions:
  - id: internal
    name: Internal
    type: 61
    url_pattern: '/internal/(.+)$'
    benchmark_url: 'dists/stable/Release'
    official_url: OFFICIAL
    cache_rules:
      - pattern: '.*'
        cache_control: 'max-age=300'
        rewrite: true
    mirrors:
      official: ['https://mirror.example.com/internal/']
`

func TestOfficialURLIsOptionalNormalizedAndValidated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "omitted", value: `""`, want: ""},
		{name: "https", value: "https://repo.example.com/internal/", want: "https://repo.example.com/internal/"},
		{name: "http with port", value: "http://repo.example.com:8080/internal/", want: "http://repo.example.com:8080/internal/"},
		{name: "without scheme", value: "repo.example.com/internal/", want: "https://repo.example.com/internal/"},
		{name: "surrounding space", value: `" repo.example.com/internal/ "`, want: "https://repo.example.com/internal/"},
		{name: "other scheme", value: "ftp://token:secret@repo.example.com/internal/", wantErr: true},
		{name: "no host", value: "https:///internal/", wantErr: true},
		{name: "unparsable", value: `"https://repo.example.com/%zz"`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempYAML(t, strings.Replace(officialURLDistributionYAML, "OFFICIAL", tc.value, 1))
			cfg, err := NewLoader(path).Load()
			if tc.wantErr {
				// The address may carry credentials and the error is logged.
				if err == nil || !strings.Contains(err.Error(), "invalid official URL") || strings.Contains(err.Error(), "repo.example.com") {
					t.Fatalf("Load = %v, want an official URL error that does not repeat the value", err)
				}
				// Registering the entry directly is validated the same way.
				raw := strings.Trim(tc.value, `"`)
				direct := &DistributionConfig{ID: "internal", Name: "Internal", Type: 61, URLPattern: `/internal/(.+)$`, BenchmarkURL: "x", OfficialURL: raw}
				if err := NewRegistry().LoadFromConfig(direct); err == nil || !strings.Contains(err.Error(), "invalid official URL") || strings.Contains(err.Error(), "repo.example.com") {
					t.Fatalf("LoadFromConfig = %v, want an official URL error that does not repeat the value", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			reg := NewRegistry()
			if err := reg.LoadFromConfig(&cfg.Distributions[0]); err != nil {
				t.Fatalf("LoadFromConfig: %v", err)
			}
			dist, ok := reg.GetByType(61)
			if !ok {
				t.Fatal("distribution not registered")
			}
			if dist.OfficialURL != tc.want {
				t.Errorf("OfficialURL = %q, want %q", dist.OfficialURL, tc.want)
			}
		})
	}
}

// An entry that reconfigures a built-in type replaces it as a whole. It does
// not inherit the built-in official address, so a file written before the
// field existed keeps starting on its first listed mirror.
func TestBuiltinTypeFromConfigDoesNotInheritOfficialURL(t *testing.T) {
	const body = `distributions:
  - id: debian
    name: Debian
    type: 3
    url_pattern: '/debian(-security)?/(.+)$'
    benchmark_url: 'dists/stable/Release'
    cache_rules:
      - pattern: '.*'
        cache_control: 'max-age=300'
        rewrite: true
    mirrors:
      official: ['https://mirror.example.com/debian/']
`
	path := writeTempYAML(t, body)
	reg := NewBuiltinRegistry()
	if err := reg.Reload(path); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	debian, _ := reg.GetByType(TypeDebian)
	if debian.Builtin || debian.OfficialURL != "" {
		t.Errorf("reconfigured Debian: Builtin=%v OfficialURL=%q, want an entry without an official address", debian.Builtin, debian.OfficialURL)
	}
	if alpine, _ := reg.GetByType(TypeAlpine); alpine.OfficialURL != AlpineOfficialURL {
		t.Errorf("untouched Alpine: OfficialURL = %q, want the built-in %q", alpine.OfficialURL, AlpineOfficialURL)
	}

	// An invalid address fails the reload and leaves the registry as it was.
	broken := strings.Replace(body, "benchmark_url:", "official_url: 'ftp://mirror.example.com/debian/'\n    benchmark_url:", 1)
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reg.Reload(path); err == nil || !strings.Contains(err.Error(), "invalid official URL") {
		t.Fatalf("Reload = %v, want an official URL error", err)
	}
	if after, _ := reg.GetByType(TypeDebian); after != debian {
		t.Error("failed reload replaced the previous Debian entry")
	}
}
