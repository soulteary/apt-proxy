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

package passthrough

import "testing"

func TestParseAcceptedForms(t *testing.T) {
	list, err := Parse([]string{
		"ppa.launchpad.net",
		"https://download.docker.com",
		"http://repo.example.test",
		"archive.example.test:8080",
		"  deb.nodesource.com  ",
		"trailing.example.test/",
		"MiXeD.Example.Test",
		"",                  // skipped
		"ppa.launchpad.net", // duplicate, collapsed
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rules := list.Rules()
	if len(rules) != 7 {
		t.Fatalf("parsed %d rules, want 7: %+v", len(rules), rules)
	}

	byHost := make(map[string]Rule, len(rules))
	for _, r := range rules {
		byHost[r.Host] = r
	}
	if !byHost["download.docker.com"].ForceHTTPS {
		t.Error("https:// entry must force TLS upstream")
	}
	if byHost["repo.example.test"].ForceHTTPS {
		t.Error("http:// entry must not force TLS")
	}
	if got := byHost["archive.example.test"].Port; got != "8080" {
		t.Errorf("port = %q, want 8080", got)
	}
	if _, ok := byHost["mixed.example.test"]; !ok {
		t.Error("host must be lower-cased")
	}
}

func TestParseRejectsUnsafeOrMalformed(t *testing.T) {
	for _, entry := range []string{
		"localhost",
		"api.localhost",
		"127.0.0.1",
		"::1",
		"10.0.0.5",
		"192.168.1.1",
		"172.16.0.1",
		"169.254.169.254", // cloud metadata
		"0.0.0.0",
		"*.launchpad.net",
		"example.test/ubuntu",
		"example.test/ubuntu/dists",
		"ftp://example.test",
		"user@example.test",
		"example.test:notaport",
		"http://",
		"example.test?q=1",
		"example.test#frag",
	} {
		if _, err := Parse([]string{entry}); err == nil {
			t.Errorf("Parse(%q) must be rejected", entry)
		}
	}
}

func TestMatch(t *testing.T) {
	list, err := Parse([]string{"ppa.launchpad.net", "https://download.docker.com", "archive.example.test:8080"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	tests := []struct {
		authority string
		want      bool
		https     bool
	}{
		{"ppa.launchpad.net", true, false},
		{"ppa.launchpad.net:80", true, false},
		{"ppa.launchpad.net:443", true, false},
		{"PPA.Launchpad.NET", true, false},
		{"download.docker.com", true, true},
		{"archive.example.test:8080", true, false},

		{"ppa.launchpad.net:9200", false, false}, // non-default port, entry had none
		{"archive.example.test", false, false},   // entry pinned :8080
		{"archive.example.test:80", false, false},
		{"evil.test", false, false},
		{"ppa.launchpad.net.evil.test", false, false},
		{"notppa.launchpad.net", false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		rule, ok := list.Match(tt.authority)
		if ok != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.authority, ok, tt.want)
			continue
		}
		if ok && rule.ForceHTTPS != tt.https {
			t.Errorf("Match(%q) ForceHTTPS = %v, want %v", tt.authority, rule.ForceHTTPS, tt.https)
		}
	}
}

// An unconfigured list must allow nothing -- that is the default posture.
func TestEmptyListAllowsNothing(t *testing.T) {
	var nilList *List
	if !nilList.Empty() {
		t.Error("nil list must be empty")
	}
	if _, ok := nilList.Match("ppa.launchpad.net"); ok {
		t.Error("nil list must not match")
	}

	empty, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil): %v", err)
	}
	if !empty.Empty() {
		t.Error("Parse(nil) must produce an empty list")
	}
	if _, ok := empty.Match("ppa.launchpad.net"); ok {
		t.Error("empty list must not match")
	}
}
