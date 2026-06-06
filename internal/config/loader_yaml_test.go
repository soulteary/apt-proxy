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
	"testing"
)

func TestExpandConfigEnv_StrictBraceForm(t *testing.T) {
	t.Setenv("APT_PROXY_TEST_USER", "alice")

	src := `user: ${APT_PROXY_TEST_USER}` + "\n" +
		`pass: $not_expanded` + "\n" +
		`raw: literal$dollar`
	got := expandConfigEnv(src)
	want := "user: alice\npass: $not_expanded\nraw: literal$dollar"
	if got != want {
		t.Fatalf("expandConfigEnv mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExpandConfigEnv_PreservesUnsetReferences(t *testing.T) {
	src := `token: ${APT_PROXY_TEST_DEFINITELY_UNSET}`
	got := expandConfigEnv(src)
	if got != src {
		t.Fatalf("unset ${VAR} should be preserved verbatim, got %q", got)
	}
}

func TestExpandConfigEnv_DefaultValue(t *testing.T) {
	t.Setenv("APT_PROXY_TEST_PRESENT", "yes")

	src := `a: ${APT_PROXY_TEST_PRESENT:-fallback}` + "\n" +
		`b: ${APT_PROXY_TEST_MISSING:-fallback}`
	got := expandConfigEnv(src)
	want := "a: yes\nb: fallback"
	if got != want {
		t.Fatalf("expandConfigEnv default mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

func TestExpandConfigEnv_PasswordWithDollar(t *testing.T) {
	src := `api_key: "p$$w0rd$abc"`
	got := expandConfigEnv(src)
	if got != src {
		t.Fatalf("password containing $ should not be touched, got %q", got)
	}
}
