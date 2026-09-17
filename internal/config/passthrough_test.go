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
	"strings"
	"testing"
)

func validatableConfig(t *testing.T, passthrough ...string) *Config {
	t.Helper()
	return &Config{
		Listen:      "127.0.0.1:3142",
		CacheDir:    t.TempDir(),
		Passthrough: passthrough,
	}
}

// A malformed or unsafe allowlist must stop startup. Tolerating it at request
// time would mean serving a list that is not the one the operator wrote.
func TestValidateConfigRejectsBadPassthrough(t *testing.T) {
	for _, entry := range []string{
		"127.0.0.1",
		"10.0.0.5",
		"localhost",
		"169.254.169.254",
		"*.launchpad.net",
		"example.test/ubuntu",
	} {
		err := ValidateConfig(validatableConfig(t, entry))
		if err == nil {
			t.Errorf("ValidateConfig with passthrough %q must fail", entry)
			continue
		}
		if !strings.Contains(err.Error(), "passthrough") {
			t.Errorf("entry %q: error %q should name the offending setting", entry, err)
		}
	}
}

func TestValidateConfigAcceptsGoodPassthrough(t *testing.T) {
	cfg := validatableConfig(t, "ppa.launchpad.net", "https://download.docker.com", "archive.example.test:8080")
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
}

// No allowlist is the default, and must stay valid.
func TestValidateConfigAllowsEmptyPassthrough(t *testing.T) {
	if err := ValidateConfig(validatableConfig(t)); err != nil {
		t.Fatalf("ValidateConfig with no passthrough: %v", err)
	}
}

// YAML and CLI must both reach Config.Passthrough, with the flag winning.
func TestPassthroughMergePrecedence(t *testing.T) {
	yaml := &Config{Passthrough: []string{"from-yaml.example.test"}}
	cli := &Config{Passthrough: []string{"from-cli.example.test"}}

	merged := MergeConfigsWithExplicit(yaml, cli, &cliExplicit{Passthrough: true})
	if len(merged.Passthrough) != 1 || merged.Passthrough[0] != "from-cli.example.test" {
		t.Errorf("explicit flag must win, got %v", merged.Passthrough)
	}

	kept := MergeConfigsWithExplicit(yaml, &Config{}, &cliExplicit{})
	if len(kept.Passthrough) != 1 || kept.Passthrough[0] != "from-yaml.example.test" {
		t.Errorf("YAML value must survive when the flag is not set, got %v", kept.Passthrough)
	}
}
