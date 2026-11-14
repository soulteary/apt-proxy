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

import "testing"

// TestPassthroughFollowRedirectsSetting covers where the switch can be set.
// Redirects are followed unless a source says otherwise, and the command line
// and the environment take precedence over the config file.
func TestPassthroughFollowRedirectsSetting(t *testing.T) {
	const off = "passthrough_follow_redirects: false\n"
	for _, tc := range []struct {
		name string
		yaml string
		args []string
		env  string
		want bool
	}{
		{name: "default", want: true},
		{name: "file without the key", yaml: "passthrough:\n  - pkgs.k8s.io\n", want: true},
		{name: "file turns it off", yaml: off, want: false},
		{name: "file states the default", yaml: "passthrough_follow_redirects: true\n", want: true},
		{name: "flag turns it off", args: []string{"--passthrough-follow-redirects=false"}, want: false},
		{name: "environment turns it off", env: "false", want: false},
		{name: "flag overrides the file", yaml: off, args: []string{"--passthrough-follow-redirects=true"}, want: true},
		{name: "flag without a value overrides the file", yaml: off, args: []string{"--passthrough-follow-redirects"}, want: true},
		{name: "environment overrides the file", yaml: off, env: "true", want: true},
		{name: "flag overrides the environment", args: []string{"--passthrough-follow-redirects=true"}, env: "false", want: true},
		{name: "blank environment value keeps the file", yaml: off, env: " ", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			if tc.env != "" {
				t.Setenv(EnvPassthroughFollowRedirects, tc.env)
			}
			cfg, err := parseWithConfigFile(t, tc.yaml, tc.args...)
			if err != nil {
				t.Fatalf("ParseFlagsWithConfigFile: %v", err)
			}
			if cfg.PassthroughFollowRedirects != tc.want {
				t.Errorf("PassthroughFollowRedirects = %t, want %t", cfg.PassthroughFollowRedirects, tc.want)
			}
		})
	}
}

// Without a config file the command line alone decides.
func TestPassthroughFollowRedirectsFlag(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"--passthrough-follow-redirects=false"}, false},
	} {
		clearConfigEnv(t)
		withArgs(t, append([]string{"apt-proxy"}, tc.args...), func() {
			cfg, err := ParseFlags()
			if err != nil {
				t.Fatalf("ParseFlags(%v): %v", tc.args, err)
			}
			if cfg.PassthroughFollowRedirects != tc.want {
				t.Errorf("ParseFlags(%v): PassthroughFollowRedirects = %t, want %t", tc.args, cfg.PassthroughFollowRedirects, tc.want)
			}
		})
	}
}

// The merge without an explicit mask cannot tell an unset false from a
// deliberate one, so it only ever turns the switch on.
func TestMergeConfigsKeepsPassthroughFollowRedirects(t *testing.T) {
	on, off := &Config{PassthroughFollowRedirects: true}, &Config{}
	for _, tc := range []struct {
		name           string
		base, override *Config
		want           bool
	}{
		{"override on", off, on, true},
		{"base on", on, off, true},
		{"both off", off, off, false},
	} {
		if got := MergeConfigs(tc.base, tc.override).PassthroughFollowRedirects; got != tc.want {
			t.Errorf("%s: PassthroughFollowRedirects = %t, want %t", tc.name, got, tc.want)
		}
	}
}
