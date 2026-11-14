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
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

// proxmoxType is the type of the custom distribution in registryWithProxmox.
const proxmoxType = 100

// registryWithProxmox returns the built-in distributions and one custom
// distribution, as a loaded distributions.yaml would leave the registry.
func registryWithProxmox(t *testing.T) *distro.Registry {
	t.Helper()
	reg := distro.NewBuiltinRegistry()
	if err := reg.Register(&distro.RegisteredDistribution{
		ID: "proxmox", Name: "Proxmox", Type: proxmoxType, URLPattern: regexp.MustCompile(`/proxmox/(.+)$`),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return reg
}

// The mode is a set in each of its three sources. The loaders keep the values
// as written; only empty items are dropped and surrounding space removed.
func TestModeIsReadAsAListFromEverySource(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		args []string
		env  map[string]string
		want []string
	}{
		{name: "not set"},

		// The config file takes a scalar or a list.
		{name: "yaml scalar", yaml: "mode: debian\n", want: []string{"debian"}},
		{name: "yaml flow list", yaml: "mode: [debian, proxmox]\n", want: []string{"debian", "proxmox"}},
		{name: "yaml block list", yaml: "mode:\n  - debian\n  - proxmox\n", want: []string{"debian", "proxmox"}},
		{name: "yaml scalar with commas", yaml: "mode: \"debian, proxmox\"\n", want: []string{"debian", "proxmox"}},
		{name: "yaml list items with commas", yaml: "mode: [\"debian,proxmox\", ubuntu]\n", want: []string{"debian", "proxmox", "ubuntu"}},
		{name: "yaml spaces, empty items and duplicates", yaml: "mode: [\" debian \", \"\", debian, \" \"]\n", want: []string{"debian", "debian"}},
		{name: "yaml list through an anchor", yaml: "mode: [&d debian, *d, proxmox]\n", want: []string{"debian", "debian", "proxmox"}},
		{name: "yaml null", yaml: "mode:\n"},
		{name: "yaml empty string", yaml: "mode: \"\"\n"},
		{name: "yaml empty list", yaml: "mode: []\n"},
		{name: "yaml number", yaml: "mode: 3\n", want: []string{"3"}},
		{
			name: "yaml value from an environment reference",
			yaml: "mode: ${TEST_APT_PROXY_MODES}\n",
			env:  map[string]string{"TEST_APT_PROXY_MODES": "debian,proxmox"},
			want: []string{"debian", "proxmox"},
		},

		// The flag and the environment variable are comma-separated.
		{name: "flag", args: []string{"--mode=debian,proxmox"}, want: []string{"debian", "proxmox"}},
		{name: "flag with one value", args: []string{"--mode=debian"}, want: []string{"debian"}},
		{name: "flag spaces, empty items and duplicates", args: []string{"--mode= debian , ,proxmox,debian,"}, want: []string{"debian", "proxmox", "debian"}},
		{name: "environment", env: map[string]string{EnvMode: "debian,proxmox"}, want: []string{"debian", "proxmox"}},
		{name: "environment with spaces", env: map[string]string{EnvMode: " debian , proxmox "}, want: []string{"debian", "proxmox"}},
		// Nothing is checked yet: the value may be an ID from distributions.yaml.
		{name: "unknown value is kept", args: []string{"--mode=debain"}, want: []string{"debain"}},

		// Flag over environment variable over file. The whole set is replaced.
		{name: "file only", yaml: "mode: [ubuntu, centos]\n", want: []string{"ubuntu", "centos"}},
		{
			name: "environment over file",
			yaml: "mode: [ubuntu, centos]\n",
			env:  map[string]string{EnvMode: "debian,proxmox"},
			want: []string{"debian", "proxmox"},
		},
		{
			name: "flag over environment and file",
			yaml: "mode: [ubuntu, centos]\n",
			args: []string{"--mode=alpine"},
			env:  map[string]string{EnvMode: "debian,proxmox"},
			want: []string{"alpine"},
		},
		{name: "flag over environment", args: []string{"--mode=alpine"}, env: map[string]string{EnvMode: "debian"}, want: []string{"alpine"}},
		{
			name: "all on the flag replaces the file's set",
			yaml: "mode: [ubuntu, centos]\n",
			args: []string{"--mode=all"},
			want: []string{"all"},
		},

		// A source that names no distribution is the same as one that is not
		// set. It must not replace the next source with the default.
		{name: "empty environment keeps the file", yaml: "mode: [debian, proxmox]\n", env: map[string]string{EnvMode: ""}, want: []string{"debian", "proxmox"}},
		{name: "blank environment keeps the file", yaml: "mode: [debian, proxmox]\n", env: map[string]string{EnvMode: "  "}, want: []string{"debian", "proxmox"}},
		{name: "separators in the environment keep the file", yaml: "mode: debian\n", env: map[string]string{EnvMode: " , "}, want: []string{"debian"}},
		{name: "empty flag keeps the file", yaml: "mode: debian\n", args: []string{"--mode="}, want: []string{"debian"}},
		{name: "empty flag falls back to the environment", yaml: "mode: debian\n", args: []string{"--mode="}, env: map[string]string{EnvMode: "alpine"}, want: []string{"alpine"}},
		{name: "empty flag and environment without a file", args: []string{"--mode=,"}, env: map[string]string{EnvMode: ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			cfg, err := parseWithConfigFile(t, tc.yaml, tc.args...)
			if err != nil {
				t.Fatalf("ParseFlagsWithConfigFile: %v", err)
			}
			if !reflect.DeepEqual(cfg.Modes, tc.want) {
				t.Errorf("Modes = %q, want %q", cfg.Modes, tc.want)
			}
			// The loaders never use the single-type form.
			if cfg.Mode != distro.TypeAllDistros {
				t.Errorf("Mode = %d, want it left unset", cfg.Mode)
			}
		})
	}
}

// ParseFlags has no config file, and reads the flag and the environment
// variable the same way.
func TestParseFlagsReadsModeList(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		env  string
		want []string
	}{
		{name: "flag", args: []string{"--mode=debian, proxmox"}, want: []string{"debian", "proxmox"}},
		{name: "environment", env: "debian,proxmox", want: []string{"debian", "proxmox"}},
		{name: "flag over environment", args: []string{"--mode=alpine"}, env: "debian,proxmox", want: []string{"alpine"}},
		{name: "empty flag falls back to the environment", args: []string{"--mode="}, env: "debian", want: []string{"debian"}},
		{name: "neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			if tc.env != "" {
				t.Setenv(EnvMode, tc.env)
			}
			withArgs(t, append([]string{"apt-proxy"}, tc.args...), func() {
				cfg, err := ParseFlags()
				if err != nil {
					t.Fatalf("ParseFlags: %v", err)
				}
				if !reflect.DeepEqual(cfg.Modes, tc.want) {
					t.Errorf("Modes = %q, want %q", cfg.Modes, tc.want)
				}
			})
		})
	}
}

// A mode that is neither a scalar nor a list of scalars is a configuration
// error with the line it is on.
func TestModeYAMLRejectsOtherShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want []string
	}{
		{"mapping", "server:\n  port: 3142\nmode:\n  debian: true\n", []string{"line 4: mode must be a distribution ID or a list of distribution IDs"}},
		{"list of lists", "mode: [[debian]]\n", []string{"line 1: cannot unmarshal !!seq into string"}},
		{"list of mappings", "mode:\n  - id: debian\n", []string{"line 2: cannot unmarshal !!map into string"}},
		// The mode error does not hide another mistake in the same file.
		{"mapping next to an unknown field", "mode: {debian: true}\nmodes: [debian]\n", []string{"line 1: mode must be", "line 2: field modes not found"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfigFile(writeConfigFile(t, tc.yaml))
			if err == nil {
				t.Fatalf("LoadConfigFile accepted the mode, Modes = %q", cfg.Modes)
			}
			for _, text := range tc.want {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error %q does not contain %q", err, text)
				}
			}
		})
	}
}

// ResolveMode is where the values meet the registry: IDs from
// distributions.yaml are valid, anything the registry does not have is not.
func TestResolveMode(t *testing.T) {
	reg := registryWithProxmox(t)
	const available = "alpine, centos, debian, proxmox, ubuntu, ubuntu-ports"

	for _, tc := range []struct {
		name string
		cfg  Config
		// want lists the enabled types; nil means every distribution.
		want    []int
		wantErr []string
	}{
		{name: "nothing set", cfg: Config{}},
		{name: "all", cfg: Config{Modes: []string{"all"}}},
		{name: "one built-in", cfg: Config{Modes: []string{"debian"}}, want: []int{distro.TypeDebian}},
		{name: "built-in and custom", cfg: Config{Modes: []string{"debian", "proxmox"}}, want: []int{distro.TypeDebian, proxmoxType}},
		{name: "custom alone", cfg: Config{Modes: []string{"proxmox"}}, want: []int{proxmoxType}},
		{name: "letter case, spaces and duplicates", cfg: Config{Modes: []string{" Debian", "PROXMOX ", "debian"}}, want: []int{distro.TypeDebian, proxmoxType}},
		{name: "single type of a directly built Config", cfg: Config{Mode: distro.TypeUbuntu}, want: []int{distro.TypeUbuntu}},
		{name: "Modes takes precedence over Mode", cfg: Config{Mode: distro.TypeUbuntu, Modes: []string{"debian"}}, want: []int{distro.TypeDebian}},
		{name: "all in Modes takes precedence over Mode", cfg: Config{Mode: distro.TypeUbuntu, Modes: []string{"all"}}},
		{
			name:    "typo of a built-in",
			cfg:     Config{Modes: []string{"debain"}},
			wantErr: []string{`unknown distribution "debain"`, available},
		},
		{
			name:    "unknown ID next to valid ones",
			cfg:     Config{Modes: []string{"debian", "proxmox", "pve"}},
			wantErr: []string{`unknown distribution "pve"`, available},
		},
		{
			name:    "all combined with a distribution",
			cfg:     Config{Modes: []string{"all", "proxmox"}},
			wantErr: []string{`"all" cannot be combined with other distributions`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode, err := ResolveMode(&tc.cfg, reg)
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("ResolveMode = %v, want an error", mode.Types(reg))
				}
				for _, text := range tc.wantErr {
					if !strings.Contains(err.Error(), text) {
						t.Errorf("error %q does not contain %q", err, text)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveMode: %v", err)
			}
			if mode.All() != (tc.want == nil) {
				t.Fatalf("All() = %v, want %v", mode.All(), tc.want == nil)
			}
			if tc.want != nil && !reflect.DeepEqual(mode.Types(reg), tc.want) {
				t.Fatalf("enabled types = %v, want %v", mode.Types(reg), tc.want)
			}
		})
	}

	// A custom ID is unknown until the registry holds distributions.yaml.
	if _, err := ResolveMode(&Config{Modes: []string{"proxmox"}}, distro.NewBuiltinRegistry()); err == nil {
		t.Error("an ID the registry does not have was accepted")
	}
	if _, err := ResolveMode(&Config{Modes: []string{"proxmox"}}, nil); err == nil {
		t.Error("a custom ID was accepted without a registry")
	}
}

// ApplyToState is the validation the server runs at startup and on each
// reload. A mode it cannot resolve leaves the state as it was, so a failed
// reload keeps the state that the routing in service was built from.
func TestApplyToStateValidatesMode(t *testing.T) {
	reg := registryWithProxmox(t)
	st := state.NewAppState()
	valid := &Config{
		Modes:   []string{"debian", "proxmox"},
		Mirrors: MirrorConfig{Debian: "http://first.example/debian/"},
	}
	if err := ApplyToState(valid, st, reg); err != nil {
		t.Fatalf("ApplyToState: %v", err)
	}
	wantTypes := []int{distro.TypeDebian, proxmoxType}
	if got := st.GetProxyMode().Types(reg); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("enabled types = %v, want %v", got, wantTypes)
	}

	// The registry as a reload leaves it when the file no longer defines
	// the custom distribution.
	err := ApplyToState(&Config{
		Modes:   []string{"debian", "proxmox"},
		Mirrors: MirrorConfig{Debian: "http://second.example/debian/"},
	}, st, distro.NewBuiltinRegistry())
	if err == nil || !strings.Contains(err.Error(), `unknown distribution "proxmox"`) {
		t.Fatalf("ApplyToState error = %v, want the missing ID named", err)
	}
	if got := st.GetProxyMode().Types(reg); !reflect.DeepEqual(got, wantTypes) {
		t.Errorf("enabled types after the failure = %v, want %v", got, wantTypes)
	}
	if got := st.GetMirror(distro.TypeDebian); got == nil || got.Host != "first.example" {
		t.Errorf("Debian mirror after the failure = %v, want the one applied before", got)
	}

	// A reload can give an ID another type. The state follows the ID.
	moved := distro.NewBuiltinRegistry()
	if err := moved.Register(&distro.RegisteredDistribution{ID: "proxmox", Type: proxmoxType + 1}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := ApplyToState(valid, st, moved); err != nil {
		t.Fatalf("ApplyToState: %v", err)
	}
	if mode := st.GetProxyMode(); !mode.Has(proxmoxType+1) || mode.Has(proxmoxType) {
		t.Errorf("enabled types = %v, want the new type of proxmox", mode.Types(moved))
	}
}

// Mode and Modes are two forms of one setting, so a merge moves them
// together. Otherwise the form left behind by the base would come back into
// effect.
func TestMergeReplacesBothModeForms(t *testing.T) {
	explicit := &cliExplicit{Mode: true}
	for _, tc := range []struct {
		name      string
		base      Config
		override  Config
		ex        *cliExplicit
		wantMode  int
		wantModes []string
	}{
		{
			name:      "explicit set replaces a set",
			base:      Config{Modes: []string{"debian", "proxmox"}},
			override:  Config{Modes: []string{"ubuntu"}},
			ex:        explicit,
			wantModes: []string{"ubuntu"},
		},
		{
			name:      "explicit set replaces a single type",
			base:      Config{Mode: distro.TypeUbuntu},
			override:  Config{Modes: []string{"debian"}},
			ex:        explicit,
			wantModes: []string{"debian"},
		},
		{
			name:     "explicit single type replaces a set",
			base:     Config{Modes: []string{"debian", "proxmox"}},
			override: Config{Mode: distro.TypeAlpine},
			ex:       explicit,
			wantMode: distro.TypeAlpine,
		},
		{
			name:      "not explicit keeps the base",
			base:      Config{Modes: []string{"debian", "proxmox"}},
			override:  Config{Modes: []string{"ubuntu"}},
			ex:        &cliExplicit{},
			wantModes: []string{"debian", "proxmox"},
		},
		{
			name:      "without a mask a set replaces a single type",
			base:      Config{Mode: distro.TypeUbuntu},
			override:  Config{Modes: []string{"debian"}},
			wantModes: []string{"debian"},
		},
		{
			name:     "without a mask a single type replaces a set",
			base:     Config{Modes: []string{"debian", "proxmox"}},
			override: Config{Mode: distro.TypeAlpine},
			wantMode: distro.TypeAlpine,
		},
		{
			name:      "without a mask an unset mode keeps the base",
			base:      Config{Modes: []string{"debian", "proxmox"}},
			override:  Config{Debug: true},
			wantModes: []string{"debian", "proxmox"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeConfigsWithExplicit(&tc.base, &tc.override, tc.ex)
			if got.Mode != tc.wantMode || !reflect.DeepEqual(got.Modes, tc.wantModes) {
				t.Fatalf("Mode = %d, Modes = %q; want %d and %q", got.Mode, got.Modes, tc.wantMode, tc.wantModes)
			}
			// The merged list is the result's own.
			if len(tc.override.Modes) > 0 && len(got.Modes) > 0 && &got.Modes[0] == &tc.override.Modes[0] {
				t.Error("merged Modes shares storage with the override")
			}
		})
	}
}
