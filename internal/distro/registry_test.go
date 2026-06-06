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
	"regexp"
	"testing"
)

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	dist := &RegisteredDistribution{
		ID:           "test",
		Name:         "Test",
		Type:         9999,
		URLPattern:   regexp.MustCompile(`^/test/`),
		BenchmarkURL: "https://example.com/ping",
	}

	if err := r.Register(dist); err != nil {
		t.Fatalf("register: %v", err)
	}

	if got, ok := r.GetByID("test"); !ok || got.Name != "Test" {
		t.Errorf("GetByID: ok=%v got=%v", ok, got)
	}
	if got, ok := r.GetByType(9999); !ok || got.ID != "test" {
		t.Errorf("GetByType: ok=%v got=%v", ok, got)
	}
}

func TestRegistryRequiresID(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&RegisteredDistribution{Type: 1})
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestRegistryRejectsZeroTypeForNonAll(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&RegisteredDistribution{ID: "ubuntu", Type: 0})
	if err == nil {
		t.Error("expected error for zero type with non-'all' ID")
	}
}

func TestRegistryAllowsTypeZeroForAll(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&RegisteredDistribution{ID: "all", Type: 0, Name: "All"}); err != nil {
		t.Fatalf("register all: %v", err)
	}
	if _, ok := r.GetByID("all"); !ok {
		t.Error("expected to find 'all' by ID")
	}
}

func TestRegistryDuplicateTypeConflict(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A"}); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if err := r.Register(&RegisteredDistribution{ID: "b", Type: 1, Name: "B"}); err == nil {
		t.Error("expected conflict registering different ID with same type")
	}
}

func TestRegistryReregisterSameIDSameTypeOK(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A2"}); err != nil {
		t.Errorf("re-register same ID/type should succeed, got %v", err)
	}
	got, _ := r.GetByID("a")
	if got.Name != "A2" {
		t.Errorf("expected updated Name=A2, got %q", got.Name)
	}
}

func TestRegistryReregisterSameIDDifferentTypeFails(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&RegisteredDistribution{ID: "a", Type: 2, Name: "A"}); err == nil {
		t.Error("expected error re-registering same ID with different type")
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Unregister("a"); err != nil {
		t.Errorf("Unregister: %v", err)
	}
	if _, ok := r.GetByID("a"); ok {
		t.Error("expected ID to be removed")
	}
	if _, ok := r.GetByType(1); ok {
		t.Error("expected type mapping to be removed")
	}
	if err := r.Unregister("a"); err == nil {
		t.Error("expected error unregistering missing ID")
	}
}

func TestRegistryClear(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A"})
	_ = r.Register(&RegisteredDistribution{ID: "b", Type: 2, Name: "B"})
	r.Clear()
	if all := r.GetAll(); len(all) != 0 {
		t.Errorf("expected empty registry after Clear, got %d entries", len(all))
	}
}

func TestRegistryGetAllReturnsCopies(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&RegisteredDistribution{ID: "a", Type: 1, Name: "A"})

	all := r.GetAll()
	all["a"].Name = "MUTATED"

	got, _ := r.GetByID("a")
	if got.Name == "MUTATED" {
		t.Error("GetAll should return copies; mutating result must not change registry")
	}
}

func TestRegistryGetAllIsolatesSliceAndMapFields(t *testing.T) {
	r := NewRegistry()
	original := &RegisteredDistribution{
		ID:         "iso",
		Type:       42,
		Name:       "iso",
		URLPattern: regexp.MustCompile(`^/iso/`),
		Mirrors: []URLWithAlias{
			{URL: "https://m1.example.com"},
		},
		CacheRules: []Rule{{OS: 42, Pattern: regexp.MustCompile(`/a`)}},
		Aliases:    map[string]string{"x": "1"},
	}
	if err := r.Register(original); err != nil {
		t.Fatalf("register: %v", err)
	}

	snapshot := r.GetAll()["iso"]
	// Mutate the snapshot's collections; original must remain untouched.
	snapshot.Mirrors = append(snapshot.Mirrors, URLWithAlias{URL: "https://m2.example.com"})
	snapshot.CacheRules = append(snapshot.CacheRules, Rule{OS: 42, Pattern: regexp.MustCompile(`/b`)})
	snapshot.Aliases["x"] = "MUTATED"
	snapshot.Aliases["y"] = "added"

	again, _ := r.GetByID("iso")
	if len(again.Mirrors) != 1 {
		t.Errorf("Mirrors length leaked: got %d, want 1", len(again.Mirrors))
	}
	if len(again.CacheRules) != 1 {
		t.Errorf("CacheRules length leaked: got %d, want 1", len(again.CacheRules))
	}
	if again.Aliases["x"] != "1" {
		t.Errorf("Aliases value leaked: got %q, want %q", again.Aliases["x"], "1")
	}
	if _, ok := again.Aliases["y"]; ok {
		t.Error("Aliases key leaked into registry")
	}
}
