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
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeConfigFile stores body as an apt-proxy.yaml in a temporary directory.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), DefaultConfigFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// unsetEnv removes the variables for the duration of the test. t.Setenv
// registers the restore; an empty value would still count as set.
func unsetEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

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

// A reference that is still present in a value after expansion must fail the
// load and name the variable; its literal text must never become a setting.
func TestLoadConfigFileRejectsUnresolvedReferences(t *testing.T) {
	t.Setenv("APT_PROXY_TEST_SET", "resolved")
	unsetEnv(t, "APT_PROXY_TEST_UNSET_A", "APT_PROXY_TEST_UNSET_B")

	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{"string value", "security:\n  api_key: ${APT_PROXY_TEST_UNSET_A}\n", "APT_PROXY_TEST_UNSET_A"},
		{"part of a quoted value", "mirrors:\n  ubuntu: \"https://${APT_PROXY_TEST_UNSET_A}/ubuntu/\"\n", "APT_PROXY_TEST_UNSET_A"},
		{"numeric field", "cache:\n  max_size_gb: ${APT_PROXY_TEST_UNSET_A}\n", "APT_PROXY_TEST_UNSET_A"},
		{"mapping key", "${APT_PROXY_TEST_UNSET_A}: value\n", "APT_PROXY_TEST_UNSET_A"},
		{
			"several references",
			"server:\n  host: ${APT_PROXY_TEST_SET}\nsecurity:\n  api_key: ${APT_PROXY_TEST_UNSET_B}\n" +
				"  trusted_proxies:\n    - ${APT_PROXY_TEST_UNSET_A}\n    - ${APT_PROXY_TEST_UNSET_B}\n",
			"APT_PROXY_TEST_UNSET_A, APT_PROXY_TEST_UNSET_B",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfigFile(writeConfigFile(t, tc.yaml))
			if err == nil {
				t.Fatal("LoadConfigFile accepted an unresolved reference")
			}
			for _, want := range []string{"unresolved environment variables: " + tc.want + " (", "${NAME:-default}"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if cfg != nil {
				t.Fatal("configuration with an unresolved reference was returned")
			}
		})
	}
}

// References in comments are not values, and a default resolves a reference
// whose variable is unset.
func TestLoadConfigFileIgnoresCommentsAndAppliesDefaults(t *testing.T) {
	unsetEnv(t, "APT_PROXY_TEST_UNSET_A")

	cfg, err := LoadConfigFile(writeConfigFile(t, `# Head comment: ${APT_PROXY_TEST_UNSET_A}
security:
  # Supports ${APT_PROXY_TEST_UNSET_A}
  api_key: literal-key # ${APT_PROXY_TEST_UNSET_A}
storage:
  s3:
    endpoint: "${APT_PROXY_TEST_UNSET_A:-s3.example.com}"
    region: "${APT_PROXY_TEST_UNSET_A:-}"
`))
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Security.APIKey != "literal-key" {
		t.Errorf("APIKey = %q, want literal-key", cfg.Security.APIKey)
	}
	if cfg.Storage.S3.Endpoint != "s3.example.com" || cfg.Storage.S3.Region != "" {
		t.Errorf("defaults not applied: endpoint=%q region=%q", cfg.Storage.S3.Endpoint, cfg.Storage.S3.Region)
	}
}

// A misspelled key must stop the load instead of silently dropping the policy
// it was meant to set.
func TestLoadConfigFileRejectsUnknownFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		key  string
	}{
		{"root", "debug: true\n", "debug"},
		{"section", "security:\n  download_allow_cidr: [192.0.2.0/24]\n", "download_allow_cidr"},
		{"nested section", "storage:\n  s3:\n    use_tls: true\n", "use_tls"},
		{"policy section", "upstream_access:\n  enabled: true\n  allowed_host: [mirror.example]\n", "allowed_host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfigFile(writeConfigFile(t, tc.yaml))
			if err == nil {
				t.Fatalf("LoadConfigFile accepted unknown field %q", tc.key)
			}
			for _, want := range []string{"failed to parse config file", "line "} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			// The message ends at the field name: no Go type definition follows.
			if want := "field " + tc.key + " not found"; !strings.HasSuffix(err.Error(), want) {
				t.Errorf("error %q does not end with %q", err, want)
			}
			if cfg != nil {
				t.Fatal("configuration with an unknown field was returned")
			}
		})
	}
}

// mirrors.election defaults to true, so the file has to tell an omitted key
// from an explicit false, and a value that is not a boolean must not pass for
// either.
func TestLoadConfigFileMirrorElection(t *testing.T) {
	unsetEnv(t, "APT_PROXY_TEST_ELECTION")
	for _, tc := range []struct {
		name         string
		yaml         string
		wantDisabled bool
	}{
		{"no mirrors section", "mode: debian\n", false},
		{"key omitted", "mirrors:\n  debian: https://mirror.example/debian/\n", false},
		{"true", "mirrors:\n  election: true\n", false},
		{"false", "mirrors:\n  election: false\n", true},
		{"null", "mirrors:\n  election:\n", false},
		{"default of an unset variable", "mirrors:\n  election: ${APT_PROXY_TEST_ELECTION:-false}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfigFile(writeConfigFile(t, tc.yaml))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if cfg.Mirrors.DisableElection != tc.wantDisabled {
				t.Errorf("DisableElection = %v, want %v", cfg.Mirrors.DisableElection, tc.wantDisabled)
			}
		})
	}

	cfg, err := LoadConfigFile(writeConfigFile(t, "mirrors:\n  election: sometimes\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") || cfg != nil {
		t.Fatalf("LoadConfigFile = %+v, %v; want a type error naming the line", cfg, err)
	}
}

func TestLoadConfigFileAcceptsEmptyDocument(t *testing.T) {
	want := yamlConfigToConfig(&YAMLConfig{})
	for _, body := range []string{"", "# no settings\n", "---\n", "null\n", "{}\n"} {
		t.Run(body, func(t *testing.T) {
			cfg, err := LoadConfigFile(writeConfigFile(t, body))
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if !reflect.DeepEqual(cfg, want) {
				t.Fatalf("LoadConfigFile = %+v, want the configuration of an empty document", cfg)
			}
		})
	}
}

func TestLoadConfigFileRejectsAdditionalDocuments(t *testing.T) {
	for _, tc := range []struct {
		extra string
		want  string
	}{
		{"---\nmode: ubuntu\n", "expected a single YAML document"},
		{"---\n", "expected a single YAML document"},
		{"---\ninvalid: [\n", "failed to parse config file"},
	} {
		t.Run(tc.extra, func(t *testing.T) {
			cfg, err := LoadConfigFile(writeConfigFile(t, "mode: debian\n"+tc.extra))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadConfigFile error = %v, want %q", err, tc.want)
			}
			if cfg != nil {
				t.Fatal("multi-document configuration was returned")
			}
		})
	}
}

// Every shipped apt-proxy.yaml must load under the strict schema once the
// environment variables it references are set.
func TestExampleConfigFilesLoad(t *testing.T) {
	var paths []string
	err := filepath.WalkDir(filepath.Join("..", "..", "examples"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == DefaultConfigFileName {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no example configuration files found")
	}

	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read example: %v", err)
			}
			for _, groups := range envRefPattern.FindAllStringSubmatch(string(source), -1) {
				t.Setenv(groups[1], "example")
			}
			cfg, err := LoadConfigFile(path)
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			if cfg == nil {
				t.Fatal("LoadConfigFile returned no configuration")
			}
		})
	}
}
