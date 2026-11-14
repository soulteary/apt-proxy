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
	"regexp"
	"strings"
	"testing"
)

// The types of the distributions modeTestRegistry adds to the built-in ones.
const (
	proxmoxTestType = 100
	deepinTestType  = 42
	// A custom type may be any number outside the built-in range, including
	// one that sorts ahead of the built-in types.
	lowTestType = -3
)

// modeTestRegistry returns the built-in distributions plus three custom ones,
// an entry without a URL pattern and the "all" placeholder.
func modeTestRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewBuiltinRegistry()
	for _, dist := range []*RegisteredDistribution{
		{ID: "proxmox", Type: proxmoxTestType, URLPattern: regexp.MustCompile(`/proxmox/(.+)$`)},
		{ID: "deepin", Type: deepinTestType, URLPattern: regexp.MustCompile(`/deepin/(.+)$`)},
		{ID: "low", Type: lowTestType, URLPattern: regexp.MustCompile(`/low/(.+)$`)},
		{ID: "unrouted", Type: 77},
		{ID: DistroAll, Type: TypeAllDistros},
	} {
		if err := reg.Register(dist); err != nil {
			t.Fatalf("Register(%s): %v", dist.ID, err)
		}
	}
	return reg
}

var builtinTypesInOrder = []int{TypeUbuntu, TypeUbuntuPorts, TypeDebian, TypeCentOS, TypeAlpine}

func TestModeOf(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []int
		// want is nil for a set that enables every distribution.
		want []int
	}{
		{"no type", nil, nil},
		{"the legacy value for all", []int{TypeAllDistros}, nil},
		{"all among other types", []int{TypeDebian, TypeAllDistros}, nil},
		{"one type", []int{TypeDebian}, []int{TypeDebian}},
		{"several types in any order", []int{proxmoxTestType, TypeDebian, TypeUbuntu}, []int{TypeUbuntu, TypeDebian, proxmoxTestType}},
		{"repeated type", []int{TypeDebian, TypeDebian}, []int{TypeDebian}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode := ModeOf(tc.types...)
			if mode.All() != (tc.want == nil) {
				t.Fatalf("All() = %v for %v", mode.All(), tc.types)
			}
			if tc.want == nil {
				// Every type is enabled, including one no registry knows.
				if !mode.Has(TypeDebian) || !mode.Has(9999) {
					t.Fatal("a set of all distributions does not have every type")
				}
				return
			}
			if got := mode.Types(nil); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Types() = %v, want %v", got, tc.want)
			}
			for _, distType := range tc.want {
				if !mode.Has(distType) {
					t.Errorf("Has(%d) = false", distType)
				}
			}
			if mode.Has(TypeAlpine) || mode.Has(TypeAllDistros) {
				t.Error("Has reports a type that was not named")
			}
		})
	}

	// The caller's slice is not kept: sorting it afterwards must not change
	// the set, and building the set must not reorder it.
	types := []int{TypeDebian, TypeUbuntu}
	mode := ModeOf(types...)
	if types[0] != TypeDebian {
		t.Fatal("ModeOf reordered its argument")
	}
	types[0] = TypeAlpine
	if mode.Has(TypeAlpine) || !mode.Has(TypeDebian) {
		t.Fatal("the set shares storage with the argument")
	}
}

// Rewriters are built in the order Types returns: the built-in types as they
// are registered, then the others ascending, whatever their number.
func TestModeSetTypesOrder(t *testing.T) {
	reg := modeTestRegistry(t)
	all := ModeSet{}

	want := append(append([]int(nil), builtinTypesInOrder...), lowTestType, deepinTestType, proxmoxTestType)
	for i := 0; i < 5; i++ {
		if got := all.Types(reg); !reflect.DeepEqual(got, want) {
			t.Fatalf("all.Types(reg) = %v, want %v", got, want)
		}
	}

	// The built-in types do not depend on the registry.
	for name, reg := range map[string]*Registry{"nil": nil, "empty": NewRegistry()} {
		if got := all.Types(reg); !reflect.DeepEqual(got, builtinTypesInOrder) {
			t.Errorf("all.Types(%s registry) = %v, want the built-in types", name, got)
		}
	}

	// An explicit set keeps what was named, in the same order rule. A type the
	// registry does not define is still reported.
	explicit := ModeOf(proxmoxTestType, TypeAlpine, 9999, lowTestType, TypeUbuntu)
	wantExplicit := []int{TypeUbuntu, TypeAlpine, lowTestType, proxmoxTestType, 9999}
	for name, reg := range map[string]*Registry{"populated": reg, "nil": nil} {
		if got := explicit.Types(reg); !reflect.DeepEqual(got, wantExplicit) {
			t.Errorf("explicit.Types(%s registry) = %v, want %v", name, got, wantExplicit)
		}
	}
}

func TestResolveModeSet(t *testing.T) {
	reg := modeTestRegistry(t)
	const registered = "alpine, centos, debian, deepin, low, proxmox, ubuntu, ubuntu-ports, unrouted"

	for _, tc := range []struct {
		name   string
		values []string
		// want lists the enabled types; nil means every distribution.
		want []int
		// wantErr lists text the error must contain. A case with an error has
		// no want.
		wantErr []string
	}{
		{name: "no value", values: nil},
		{name: "only empty values", values: []string{"", "  "}},
		{name: "all", values: []string{"all"}},
		{name: "all in another case", values: []string{"ALL"}},
		{name: "all repeated", values: []string{"all", " All "}},
		{name: "one built-in", values: []string{"debian"}, want: []int{TypeDebian}},
		{name: "built-in with a hyphen", values: []string{"ubuntu-ports"}, want: []int{TypeUbuntuPorts}},
		{name: "one custom", values: []string{"proxmox"}, want: []int{proxmoxTestType}},
		{name: "built-in and custom", values: []string{"proxmox", "debian"}, want: []int{TypeDebian, proxmoxTestType}},
		{name: "surrounding space and empty values", values: []string{" debian ", "", "\tproxmox\n"}, want: []int{TypeDebian, proxmoxTestType}},
		{name: "repeated value", values: []string{"debian", "debian", "proxmox", "debian"}, want: []int{TypeDebian, proxmoxTestType}},
		{name: "letter case is ignored", values: []string{"Debian", "PROXMOX", "debian"}, want: []int{TypeDebian, proxmoxTestType}},
		{name: "distribution without a URL pattern", values: []string{"unrouted"}, want: []int{77}},
		{
			name:    "typo of a built-in",
			values:  []string{"debain"},
			wantErr: []string{`invalid mode: unknown distribution "debain"`, `"all"`, registered},
		},
		{
			name:    "unknown next to a known ID",
			values:  []string{"debian", "proxmoxx"},
			wantErr: []string{`unknown distribution "proxmoxx"`, registered},
		},
		{
			name:    "several unknown values are reported together and once",
			values:  []string{"debain", "debian", "promox", "debain"},
			wantErr: []string{`unknown distributions "debain", "promox";`, registered},
		},
		{
			name:    "all with a distribution",
			values:  []string{"all", "debian"},
			wantErr: []string{`invalid mode: "all" cannot be combined with other distributions (got all, debian)`},
		},
		{
			name:    "all after a distribution, in another case",
			values:  []string{"debian", "All"},
			wantErr: []string{`"all" cannot be combined with other distributions (got debian, All)`},
		},
		{
			name:    "all with an unknown value reports the unknown value",
			values:  []string{"all", "debain"},
			wantErr: []string{`unknown distribution "debain"`},
		},
		{
			name:    "a number is not a distribution ID",
			values:  []string{"3"},
			wantErr: []string{`unknown distribution "3"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode, err := ResolveModeSet(tc.values, reg)
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("ResolveModeSet(%q) = %v, want an error", tc.values, mode.Types(reg))
				}
				for _, text := range tc.wantErr {
					if !strings.Contains(err.Error(), text) {
						t.Errorf("error %q does not contain %q", err, text)
					}
				}
				if !mode.All() {
					t.Errorf("a failed resolution returned the set %v", mode.Types(reg))
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveModeSet(%q): %v", tc.values, err)
			}
			if tc.want == nil {
				if !mode.All() {
					t.Fatalf("ResolveModeSet(%q) = %v, want every distribution", tc.values, mode.Types(reg))
				}
				return
			}
			if got := mode.Types(reg); mode.All() || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ResolveModeSet(%q) = %v (all=%v), want %v", tc.values, got, mode.All(), tc.want)
			}
		})
	}
}

// Without a registry only the built-in IDs are known, and the error says so
// by listing them.
func TestResolveModeSetWithoutRegistry(t *testing.T) {
	mode, err := ResolveModeSet([]string{"ubuntu", "alpine"}, nil)
	if err != nil {
		t.Fatalf("ResolveModeSet: %v", err)
	}
	if got, want := mode.Types(nil), []int{TypeUbuntu, TypeAlpine}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Types() = %v, want %v", got, want)
	}

	_, err = ResolveModeSet([]string{"proxmox"}, nil)
	const want = `invalid mode: unknown distribution "proxmox"; accepted values are "all" and the registered distribution IDs: alpine, centos, debian, ubuntu, ubuntu-ports`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
}

// A registry that defines nothing cannot satisfy any ID.
func TestResolveModeSetEmptyRegistry(t *testing.T) {
	if mode, err := ResolveModeSet([]string{"all"}, NewRegistry()); err != nil || !mode.All() {
		t.Fatalf("all: mode=%v err=%v", mode, err)
	}
	_, err := ResolveModeSet([]string{"ubuntu"}, NewRegistry())
	if err == nil || !strings.Contains(err.Error(), "registered distribution IDs: (none)") {
		t.Fatalf("error = %v, want it to say that no ID is registered", err)
	}
}

// IDs are compared without regard to case, which two IDs of a registry may
// make ambiguous. The exact spelling still selects one of them; any other
// spelling is refused instead of picking one at random.
func TestResolveModeSetAmbiguousCase(t *testing.T) {
	reg := NewBuiltinRegistry()
	for id, distType := range map[string]int{"Mixed": 200, "mixed": 201} {
		if err := reg.Register(&RegisteredDistribution{ID: id, Type: distType}); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
	}
	for value, want := range map[string]int{"Mixed": 200, "mixed": 201} {
		mode, err := ResolveModeSet([]string{value}, reg)
		if err != nil || !reflect.DeepEqual(mode.Types(reg), []int{want}) {
			t.Errorf("ResolveModeSet(%q) = %v, %v; want type %d", value, mode.Types(reg), err, want)
		}
	}
	_, err := ResolveModeSet([]string{"MIXED"}, reg)
	if err == nil || !strings.Contains(err.Error(), `"MIXED" matches several distribution IDs that differ only in case: Mixed, mixed`) {
		t.Fatalf("error = %v, want the ambiguity reported with both IDs", err)
	}
}

// builtinTypes repeats the built-in distributions for Types. It has to list
// each of them once, in the order RegisterBuiltins has them.
func TestBuiltinTypesCoverEveryBuiltin(t *testing.T) {
	if !reflect.DeepEqual(builtinTypes, builtinTypesInOrder) {
		t.Fatalf("builtinTypes = %v, want %v", builtinTypes, builtinTypesInOrder)
	}
	registered := NewBuiltinRegistry().GetAll()
	if len(registered) != len(builtinTypes) {
		t.Fatalf("%d built-in distributions are registered, builtinTypes lists %d", len(registered), len(builtinTypes))
	}
	for _, dist := range registered {
		if !dist.Builtin || !ModeOf(builtinTypes...).Has(dist.Type) {
			t.Errorf("built-in %s (type %d) is missing from builtinTypes", dist.ID, dist.Type)
		}
	}
}
