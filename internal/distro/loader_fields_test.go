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

const strictFieldsDistributionYAML = `distributions:
  - id: proxmox
    name: Proxmox
    type: 6
    url_pattern: '/debian/(.+)$'
    host_pattern: '^download\.proxmox\.com(:\d+)?$'
    benchmark_url: 'pbs-client/dists/trixie/Release'
    cache_rules:
      - pattern: 'deb$'
        cache_control: 'max-age=100000'
        rewrite: true
    mirrors:
      official:
        - 'http://download.proxmox.com/debian/'
`

func TestLoaderRejectsUnknownFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		old  string
		key  string
	}{
		{"root", "distributions:", "distribution"},
		{"distribution", "host_pattern:", "host-pattern"},
		{"cache rule", "cache_control:", "cache-control"},
		{"mirror list", "official:", "offical"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(strictFieldsDistributionYAML, tc.old, tc.key+":", 1)
			loader := NewLoader(writeTempYAML(t, body))
			cfg, err := loader.Load()
			if err == nil {
				t.Fatalf("Load accepted unknown field %q", tc.key)
			}
			for _, want := range []string{"failed to parse distribution config", "line ", "field " + tc.key + " not found"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if cfg != nil || loader.GetConfig() != nil {
				t.Fatal("invalid configuration was published")
			}
		})
	}
}

func TestLoaderAcceptsEmptyConfiguration(t *testing.T) {
	for _, body := range []string{"", "# no custom distributions\n", "---\n", "null\n", "distributions: []\n"} {
		t.Run(body, func(t *testing.T) {
			loader := NewLoader(writeTempYAML(t, body))
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg == nil || len(cfg.Distributions) != 0 || loader.GetConfig() != cfg {
				t.Fatalf("Load = %#v, want a published empty configuration", cfg)
			}
		})
	}
}

func TestLoaderRejectsAdditionalDocuments(t *testing.T) {
	for _, extra := range []string{"---\ndistributions: []\n", "---\n", "---\ninvalid: [\n"} {
		t.Run(extra, func(t *testing.T) {
			loader := NewLoader(writeTempYAML(t, strictFieldsDistributionYAML+extra))
			cfg, err := loader.Load()
			if err == nil || !strings.Contains(err.Error(), "failed to parse distribution config") {
				t.Fatalf("Load = %#v, %v; want a trailing document error", cfg, err)
			}
			if cfg != nil || loader.GetConfig() != nil {
				t.Fatal("multi-document configuration was published")
			}
		})
	}
}

func TestLoaderKeepsAnchorsMergesAndAliasNames(t *testing.T) {
	body := `distributions:
  - &archive
    id: first
    name: First
    type: 61
    url_pattern: '/first/(.+)$'
    benchmark_url: 'dists/stable/Release'
    cache_rules:
      - &package_rule
        pattern: 'deb$'
        cache_control: 'max-age=100000'
        rewrite: true
      - <<: *package_rule
        pattern: '.*'
        cache_control: 'max-age=300'
    mirrors:
      official: ['https://repo.example.com/']
    aliases:
      arbitrary-mirror-name: 'https://repo.example.com/'
  - <<: *archive
    id: second
    type: 62
    url_pattern: '/second/(.+)$'
`
	cfg, err := NewLoader(writeTempYAML(t, body)).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Distributions) != 2 {
		t.Fatalf("got %d distributions, want 2", len(cfg.Distributions))
	}
	for _, dist := range cfg.Distributions {
		if len(dist.CacheRules) != 2 || dist.CacheRules[0].CacheControl != "max-age=100000" ||
			dist.CacheRules[1].CacheControl != "max-age=300" || !dist.CacheRules[1].Rewrite {
			t.Errorf("merged cache rules for %q = %#v", dist.ID, dist.CacheRules)
		}
		if dist.Aliases["arbitrary-mirror-name"] != "https://repo.example.com/" {
			t.Errorf("aliases for %q = %#v", dist.ID, dist.Aliases)
		}
	}
	if cfg.Distributions[1].ID != "second" || cfg.Distributions[1].Type != 62 {
		t.Errorf("merged distribution overrides = %#v", cfg.Distributions[1])
	}
}

func TestRegistryRejectsUnknownFieldsWithoutLosingConfiguration(t *testing.T) {
	path := writeTempYAML(t, strictFieldsDistributionYAML)
	reg := NewBuiltinRegistry()
	if err := reg.Reload(path); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}
	before, ok := reg.GetByID("proxmox")
	if !ok {
		t.Fatal("initial custom distribution was not registered")
	}
	broken := strings.Replace(strictFieldsDistributionYAML, "cache_control:", "cache-control:", 1)
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := reg.Reload(path); err == nil || !strings.Contains(err.Error(), "cache-control") {
		t.Fatalf("Reload = %v, want an unknown-field error", err)
	}
	after, ok := reg.GetByID("proxmox")
	if !ok || after != before || after.CacheRules[0].CacheControl != "max-age=100000" {
		t.Fatal("failed reload replaced the previous custom distribution")
	}
	if _, ok := reg.GetByID(DistroDebian); !ok {
		t.Fatal("failed reload removed built-in Debian")
	}
}
