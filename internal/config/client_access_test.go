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

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDownloadAccessYAMLAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("security:\n  download_allow_cidrs: [192.0.2.0/24, '2001:db8::/32']\n  download_deny_cidrs: [192.0.2.42/32]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []*Config{cfg, MergeConfigs(cfg, &Config{}), MergeConfigsWithExplicit(cfg, &Config{}, &cliExplicit{})} {
		if !reflect.DeepEqual(got.Security.DownloadAllowCIDRs, []string{"192.0.2.0/24", "2001:db8::/32"}) || !reflect.DeepEqual(got.Security.DownloadDenyCIDRs, []string{"192.0.2.42/32"}) {
			t.Fatalf("lost policy: %+v", got.Security)
		}
	}
	override := &Config{Security: SecurityConfig{DownloadAllowCIDRs: []string{"127.0.0.1/32"}, DownloadDenyCIDRs: []string{}}}
	got := MergeConfigs(cfg, override)
	if len(got.Security.DownloadDenyCIDRs) != 0 || got.Security.DownloadAllowCIDRs[0] != "127.0.0.1/32" {
		t.Fatalf("override failed: %+v", got.Security)
	}
	for _, security := range []SecurityConfig{{DownloadAllowCIDRs: []string{"bad"}}, {DownloadDenyCIDRs: []string{""}}} {
		if err := ValidateConfig(&Config{Security: security}); err == nil {
			t.Fatalf("invalid policy accepted: %+v", security)
		}
	}
}

func TestDownloadAccessYAMLEmptyOverride(t *testing.T) {
	for _, tc := range []struct {
		name, yaml            string
		clearAllow, clearDeny bool
	}{
		{"omitted", "security: {}\n", false, false},
		{"clear allow", "security:\n  download_allow_cidrs: []\n", true, false},
		{"clear deny", "security:\n  download_deny_cidrs: []\n", false, true},
		{"clear both", "security:\n  download_allow_cidrs: []\n  download_deny_cidrs: []\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "override.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			override, err := LoadConfigFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if (override.Security.DownloadAllowCIDRs != nil) != tc.clearAllow || (override.Security.DownloadDenyCIDRs != nil) != tc.clearDeny {
				t.Fatalf("YAML presence lost: %+v", override.Security)
			}
			base := &Config{Security: SecurityConfig{DownloadAllowCIDRs: []string{"192.0.2.0/24"}, DownloadDenyCIDRs: []string{"192.0.2.42/32"}}}
			merged := MergeConfigs(base, override)
			if (len(merged.Security.DownloadAllowCIDRs) == 0) != tc.clearAllow || (len(merged.Security.DownloadDenyCIDRs) == 0) != tc.clearDeny {
				t.Fatalf("incorrect policy merge: %+v", merged.Security)
			}
			// A merged override must retain explicit emptiness in a subsequent merge.
			chained := MergeConfigs(base, merged)
			if !reflect.DeepEqual(chained.Security, merged.Security) {
				t.Fatalf("chained merge restored old policy: %+v", chained.Security)
			}
			if merged.Security.DownloadAllowCIDRs == nil || merged.Security.DownloadDenyCIDRs == nil {
				t.Fatal("merge collapsed explicit empty list")
			}
		})
	}
}
