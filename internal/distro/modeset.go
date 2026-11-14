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
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// builtinTypes lists the built-in distribution types in the order
// RegisterBuiltins registers them. ModeSet.Types returns them in this order,
// ahead of the types that distributions.yaml adds.
var builtinTypes = []int{TypeUbuntu, TypeUbuntuPorts, TypeDebian, TypeCentOS, TypeAlpine}

// ModeSet is the value of the mode setting: the set of distribution types a
// server serves. The zero value serves every registered distribution, which
// is what the setting's "all" means. It is expanded against the registry each
// time it is used, so it covers a distribution that a later reload adds.
//
// A ModeSet is not modified after it has been built and may be shared between
// goroutines.
type ModeSet struct {
	// types is sorted ascending and holds no duplicates. It is empty when
	// every distribution is enabled.
	types []int
}

// ModeOf returns the set of the given distribution types. As with the
// setting, naming no type or naming TypeAllDistros enables every
// distribution, so ModeOf(mode) converts the single mode value that the set
// replaced.
func ModeOf(types ...int) ModeSet {
	if len(types) == 0 || slices.Contains(types, TypeAllDistros) {
		return ModeSet{}
	}
	set := slices.Clone(types)
	slices.Sort(set)
	return ModeSet{types: slices.Compact(set)}
}

// ResolveModeSet turns the values of the mode setting into a ModeSet.
//
// A value is "all" or the ID of a distribution registered in reg, compared
// without regard to case. A nil reg knows the built-in distributions only.
// Space around a value is ignored, an empty value is skipped and a repeated
// value counts once. No value at all enables every distribution, as "all"
// does.
//
// An ID that reg does not have is an error instead of a fallback to "all": a
// misspelled ID would otherwise serve every distribution without a sign that
// the setting was not applied. "all" together with other values is an error
// as well, because it cannot be told which of the two was meant.
func ResolveModeSet(values []string, reg *Registry) (ModeSet, error) {
	if reg == nil {
		reg = NewBuiltinRegistry()
	}
	ids := reg.typesByID()

	var (
		named   []string // the values as written, without the empty ones
		types   []int
		unknown []string
		all     bool
	)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		named = append(named, value)
		if strings.EqualFold(value, DistroAll) {
			all = true
			continue
		}
		matches := matchingIDs(ids, value)
		switch len(matches) {
		case 0:
			if !slices.Contains(unknown, value) {
				unknown = append(unknown, value)
			}
		case 1:
			types = append(types, ids[matches[0]])
		default:
			return ModeSet{}, fmt.Errorf("invalid mode: %q matches several distribution IDs that differ only in case: %s",
				value, strings.Join(matches, ", "))
		}
	}

	if len(unknown) > 0 {
		noun := "distribution"
		if len(unknown) > 1 {
			noun = "distributions"
		}
		available := "(none)"
		if len(ids) > 0 {
			available = strings.Join(sortedKeys(ids), ", ")
		}
		return ModeSet{}, fmt.Errorf("invalid mode: unknown %s %s; accepted values are %q and the registered distribution IDs: %s",
			noun, quoteAll(unknown), DistroAll, available)
	}
	if all && len(types) > 0 {
		return ModeSet{}, fmt.Errorf("invalid mode: %q cannot be combined with other distributions (got %s)",
			DistroAll, strings.Join(named, ", "))
	}
	return ModeOf(types...), nil
}

// All reports whether every registered distribution is enabled.
func (m ModeSet) All() bool {
	return len(m.types) == 0
}

// Has reports whether the distribution type is enabled.
func (m ModeSet) Has(distType int) bool {
	return m.All() || slices.Contains(m.types, distType)
}

// Types returns the enabled distribution types in a fixed order: the
// built-in ones as RegisterBuiltins lists them, then the others ascending.
//
// When every distribution is enabled, the others are the types reg has a URL
// pattern for, and the built-in types are included even if reg is nil or does
// not define them. An explicit set is returned as it is: a type reg does not
// define stays in the result, so the caller can report it instead of serving
// fewer distributions than were asked for.
func (m ModeSet) Types(reg *Registry) []int {
	others := m.types
	if m.All() {
		others = reg.routableTypes()
	}
	out := make([]int, 0, len(builtinTypes)+len(others))
	for _, distType := range builtinTypes {
		if m.Has(distType) {
			out = append(out, distType)
		}
	}
	for _, distType := range others {
		if !slices.Contains(builtinTypes, distType) {
			out = append(out, distType)
		}
	}
	return out
}

// typesByID returns the type of every distribution that can be enabled by ID.
func (r *Registry) typesByID() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make(map[string]int, len(r.distributions))
	for id, dist := range r.distributions {
		// Type 0 is the "all" placeholder Register accepts. It is not a
		// distribution that could be served on its own.
		if dist.Type != TypeAllDistros {
			ids[id] = dist.Type
		}
	}
	return ids
}

// routableTypes returns, in ascending order, the types of the distributions
// that have a URL pattern. A nil registry has none.
func (r *Registry) routableTypes() []int {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var types []int
	for _, dist := range r.distributions {
		if dist.Type != TypeAllDistros && dist.URLPattern != nil {
			types = append(types, dist.Type)
		}
	}
	slices.Sort(types)
	return types
}

// matchingIDs returns the IDs that value names: the ID spelled exactly like
// it, or else every ID that differs from it only in case, in sorted order.
func matchingIDs(ids map[string]int, value string) []string {
	if _, ok := ids[value]; ok {
		return []string{value}
	}
	var matches []string
	for _, id := range sortedKeys(ids) {
		if strings.EqualFold(id, value) {
			matches = append(matches, id)
		}
	}
	return matches
}

func sortedKeys(ids map[string]int) []string {
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return keys
}

func quoteAll(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return strings.Join(quoted, ", ")
}
