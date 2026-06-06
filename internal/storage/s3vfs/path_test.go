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

package s3vfs

import "testing"

func TestNormalizePrefix(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"/":             "",
		"//":            "",
		"apt-proxy":     "apt-proxy/",
		"apt-proxy/":    "apt-proxy/",
		"/apt-proxy/":   "apt-proxy/",
		"  apt-proxy  ": "apt-proxy/",
		"a/b/c":         "a/b/c/",
		"/a/b/c/":       "a/b/c/",
	}
	for in, want := range cases {
		got := normalizePrefix(in)
		if got != want {
			t.Errorf("normalizePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToKeyAndDirPrefix(t *testing.T) {
	s := &S3VFS{prefix: normalizePrefix("apt-proxy")}

	cases := []struct {
		in      string
		wantKey string
		wantDir string
	}{
		{"/", "apt-proxy/", "apt-proxy/"},
		{"body/v1/abc", "apt-proxy/body/v1/abc", "apt-proxy/body/v1/abc/"},
		{"/header/v1/xyz", "apt-proxy/header/v1/xyz", "apt-proxy/header/v1/xyz/"},
		{"body/", "apt-proxy/body/", "apt-proxy/body/"},
	}
	for _, tc := range cases {
		if got := s.toKey(tc.in); got != tc.wantKey {
			t.Errorf("toKey(%q) = %q, want %q", tc.in, got, tc.wantKey)
		}
		if got := s.dirPrefix(tc.in); got != tc.wantDir {
			t.Errorf("dirPrefix(%q) = %q, want %q", tc.in, got, tc.wantDir)
		}
	}
}

func TestFromKey(t *testing.T) {
	s := &S3VFS{prefix: "apt-proxy/"}
	got := s.fromKey("apt-proxy/body/v1/abc")
	want := "body/v1/abc"
	if got != want {
		t.Errorf("fromKey = %q, want %q", got, want)
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"a/b/c":    "c",
		"a/b/c/":   "c",
		"hello":    "hello",
		"":         "",
		"/leading": "leading",
	}
	for in, want := range cases {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}
