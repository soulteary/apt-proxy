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
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

// withArgs swaps os.Args for the duration of fn so flag parsing tests
// don't pollute the surrounding test binary's flag set.
func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
	fn()
}

// clearConfigEnv unsets every env var the loader looks at so tests
// don't see noise from a developer's shell or from CI exports.
//
// We use t.Setenv first (so the test framework restores the prior
// value at cleanup) and *then* Unsetenv so the loader's
// `os.LookupEnv(...)` calls return ok=false rather than ok=true with
// an empty string. The latter trips flagOrEnvSet into thinking the
// user explicitly cleared the flag, which silently breaks the merge.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		EnvHost, EnvPort, EnvCacheDir, EnvMode,
		EnvCacheMaxSize, EnvCacheTTL, EnvCacheCleanupInterval,
		EnvDebug, EnvUbuntu, EnvUbuntuPorts, EnvDebian, EnvDebianSecurity,
		EnvCentOS, EnvAlpine, EnvMirrorElection,
		EnvTLSEnabled, EnvTLSCertFile, EnvTLSKeyFile, EnvPassthrough, EnvPassthroughFollowRedirects,
		EnvAPIKey, EnvAPIRateLimitPerMinute, EnvEnableAPIAuth, EnvTrustedProxies,
		EnvUpstreamKeepAlive, EnvDistributionsConfig,
		EnvStorageBackend, EnvS3Endpoint, EnvS3Region, EnvS3Bucket, EnvS3Prefix,
		EnvS3AccessKey, EnvS3SecretKey, EnvS3SessionToken, EnvS3UseSSL,
		EnvS3UsePathStyle, EnvS3InlineMaxMB, EnvS3TempDir,
		EnvConfigFile,
	} {
		t.Setenv(v, "")
		_ = os.Unsetenv(v)
	}
}

// TestParseFlagsDefaults exercises ParseFlags with no CLI input nor
// env. The returned config must have sane defaults filled in.
func TestParseFlagsDefaults(t *testing.T) {
	clearConfigEnv(t)
	withArgs(t, []string{"apt-proxy"}, func() {
		cfg, err := ParseFlags()
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		// No mode is every distribution.
		if cfg.Mode != distro.TypeAllDistros || len(cfg.Modes) != 0 {
			t.Errorf("Mode = %d, Modes = %q; want neither set", cfg.Mode, cfg.Modes)
		}
		if cfg.Listen == "" {
			t.Error("Listen should default to host:port, got empty")
		}
		if cfg.Cache.MaxSize == 0 {
			t.Error("Cache.MaxSize should be defaulted, got 0")
		}
		if cfg.Mirrors.DisableElection {
			t.Error("mirror election should be on by default")
		}
	})
}

// TestParseFlagsMirrorElection covers the loader without a config file: the
// flag wins over the environment variable, and both can turn election off.
func TestParseFlagsMirrorElection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		env         string
		wantElected bool
	}{
		{name: "flag off", args: []string{"--mirror-election=false"}, wantElected: false},
		{name: "environment off", env: "false", wantElected: false},
		{name: "flag on over environment off", args: []string{"--mirror-election=true"}, env: "false", wantElected: true},
		{name: "flag off over environment on", args: []string{"--mirror-election=false"}, env: "true", wantElected: false},
		{name: "environment zero", env: "0", wantElected: false},
		// Like every boolean variable, a value cli-kit cannot parse selects
		// the default. The documentation says so; a change has to update it.
		{name: "environment value that is not a boolean", env: "off", wantElected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			if tc.env != "" {
				t.Setenv(EnvMirrorElection, tc.env)
			}
			withArgs(t, append([]string{"apt-proxy"}, tc.args...), func() {
				cfg, err := ParseFlags()
				if err != nil {
					t.Fatalf("ParseFlags: %v", err)
				}
				if got := !cfg.Mirrors.DisableElection; got != tc.wantElected {
					t.Errorf("mirror election = %v, want %v", got, tc.wantElected)
				}
			})
		})
	}
}

func TestParseFlagsDebianSecurityMirror(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(EnvDebianSecurity, "https://env.example.com/debian-security/")
	withArgs(t, []string{"apt-proxy", "--debian-security=https://cli.example.com/debian-security/"}, func() {
		cfg, err := ParseFlags()
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if got, want := cfg.Mirrors.DebianSecurity, "https://cli.example.com/debian-security/"; got != want {
			t.Fatalf("DebianSecurity = %q, want CLI value %q", got, want)
		}
	})
}

// TestParseFlagsModeOverride drives ParseFlags with --mode=ubuntu and
// asserts the mode is preserved through the resolver.
func TestParseFlagsModeOverride(t *testing.T) {
	clearConfigEnv(t)
	withArgs(t, []string{"apt-proxy", "--mode=ubuntu", "--port=12345"}, func() {
		cfg, err := ParseFlags()
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if !reflect.DeepEqual(cfg.Modes, []string{"ubuntu"}) {
			t.Errorf("Modes = %q, want [ubuntu]", cfg.Modes)
		}
		if !strings.HasSuffix(cfg.Listen, ":12345") {
			t.Errorf("Listen = %q, want suffix :12345", cfg.Listen)
		}
	})
}

// TestParseFlagsKeepsUnknownMode covers a mode that names no built-in
// distribution. The parser cannot tell a typo from an ID that
// distributions.yaml defines, so it keeps the value; it used to drop it and
// serve every distribution. The value is rejected when it is resolved against
// the registry.
func TestParseFlagsKeepsUnknownMode(t *testing.T) {
	clearConfigEnv(t)
	for _, parse := range []func() (*Config, error){ParseFlags, ParseFlagsWithConfigFile} {
		withArgs(t, []string{"apt-proxy", "--mode=fakedistro"}, func() {
			cfg, err := parse()
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			if !reflect.DeepEqual(cfg.Modes, []string{"fakedistro"}) {
				t.Fatalf("Modes = %q, want the value as written", cfg.Modes)
			}
			if _, err := ResolveMode(cfg, distro.NewBuiltinRegistry()); err == nil || !strings.Contains(err.Error(), `"fakedistro"`) {
				t.Fatalf("ResolveMode error = %v, want the unknown value named", err)
			}
		})
	}
}

// TestParseFlagsWithConfigFile exercises the file+CLI merge: when a
// YAML config sets some fields and CLI overrides others, the result
// must reflect both layers.
func TestParseFlagsWithConfigFile(t *testing.T) {
	clearConfigEnv(t)
	yaml := `server:
  host: 0.0.0.0
  port: "6789"
mode: debian
cache:
  max_size_gb: 4
`
	dir := t.TempDir()
	path := dir + "/apt-proxy.yaml"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	withArgs(t, []string{"apt-proxy", "--config=" + path, "--mode=ubuntu"}, func() {
		cfg, err := ParseFlagsWithConfigFile()
		if err != nil {
			t.Fatalf("ParseFlagsWithConfigFile: %v", err)
		}
		// CLI mode wins over YAML.
		if !reflect.DeepEqual(cfg.Modes, []string{"ubuntu"}) {
			t.Errorf("Modes = %q, want [ubuntu]", cfg.Modes)
		}
		// YAML listen survives because CLI didn't set it.
		if cfg.Listen != "0.0.0.0:6789" {
			t.Errorf("Listen = %q, want 0.0.0.0:6789", cfg.Listen)
		}
		// YAML cache.max_size_gb survives (converted to bytes).
		const wantMaxSize int64 = 4 * 1024 * 1024 * 1024
		if cfg.Cache.MaxSize != wantMaxSize {
			t.Errorf("Cache.MaxSize = %d, want %d", cfg.Cache.MaxSize, wantMaxSize)
		}
	})
}

// TestMergeConfigsWithExplicitNilMask falls through to MergeConfigs
// when ex is nil.
func TestMergeConfigsWithExplicitNilMask(t *testing.T) {
	base := &Config{Listen: "base:80", CacheDir: "/base"}
	override := &Config{Listen: "ovr:90"}
	got := MergeConfigsWithExplicit(base, override, nil)
	if got.Listen != "ovr:90" {
		t.Errorf("Listen = %q, want ovr:90", got.Listen)
	}
	if got.CacheDir != "/base" {
		t.Errorf("CacheDir = %q, want /base (preserved from base)", got.CacheDir)
	}
}

// TestMergeConfigsWithExplicitOverridesZero exercises the "explicit
// false beats true" path that the non-explicit merge can't express.
func TestMergeConfigsWithExplicitOverridesZero(t *testing.T) {
	base := &Config{Debug: true, UpstreamKeepAlive: true, Mirrors: MirrorConfig{DisableElection: true}}
	override := &Config{Debug: false, UpstreamKeepAlive: false}
	ex := &cliExplicit{Debug: true, UpstreamKeepAlive: true, MirrorElection: true}

	got := MergeConfigsWithExplicit(base, override, ex)
	if got.Debug {
		t.Error("Debug should have been overridden to false")
	}
	if got.UpstreamKeepAlive {
		t.Error("UpstreamKeepAlive should have been overridden to false")
	}
	if got.Mirrors.DisableElection {
		t.Error("an explicit mirror election setting should have turned election back on")
	}
	if got := MergeConfigsWithExplicit(base, override, &cliExplicit{}); !got.Mirrors.DisableElection {
		t.Error("a defaulted mirror election setting replaced the file's")
	}
}

// TestMergeConfigsWithExplicitNilInputs covers the early-return paths.
func TestMergeConfigsWithExplicitNilInputs(t *testing.T) {
	cfg := &Config{Listen: "x:1"}
	ex := &cliExplicit{}
	if got := MergeConfigsWithExplicit(nil, cfg, ex); got != cfg {
		t.Error("nil base should return override unchanged")
	}
	if got := MergeConfigsWithExplicit(cfg, nil, ex); got != cfg {
		t.Error("nil override should return base unchanged")
	}
}

// TestApplyToStatePopulatesMirrors covers the success path and the
// two nil-input error paths of ApplyToState.
func TestApplyToStatePopulatesMirrors(t *testing.T) {
	st := state.NewAppState()
	cfg := &Config{
		Mode: distro.TypeUbuntu,
		Mirrors: MirrorConfig{
			Ubuntu:      "http://example.com/ubuntu/",
			UbuntuPorts: "http://example.com/ubuntu-ports/",
			Debian:      "http://example.com/debian/",
			CentOS:      "http://example.com/centos/",
			Alpine:      "http://example.com/alpine/",
		},
	}
	if err := ApplyToState(cfg, st, nil); err != nil {
		t.Fatalf("ApplyToState: %v", err)
	}
	if got := st.GetMirror(distro.TypeUbuntu); got == nil ||
		got.String() != "http://example.com/ubuntu/" {
		t.Errorf("Ubuntu mirror = %v, want example.com/ubuntu/", got)
	}
	if got := st.GetProxyMode().Types(nil); !reflect.DeepEqual(got, []int{distro.TypeUbuntu}) {
		t.Errorf("ProxyMode enables %v, want Ubuntu only", got)
	}
	if !st.MirrorElection() {
		t.Error("mirror election is off for a config that does not disable it")
	}

	// The setting is applied in both directions: a reload re-applies the
	// config to the state it configured before.
	cfg.Mirrors.DisableElection = true
	if err := ApplyToState(cfg, st, nil); err != nil {
		t.Fatalf("ApplyToState: %v", err)
	}
	if st.MirrorElection() {
		t.Error("mirror election is on for a config that disables it")
	}
	cfg.Mirrors.DisableElection = false
	if err := ApplyToState(cfg, st, nil); err != nil {
		t.Fatalf("ApplyToState: %v", err)
	}
	if !st.MirrorElection() {
		t.Error("mirror election stayed off after the config enabled it again")
	}
}

func TestApplyToStateNilArguments(t *testing.T) {
	if err := ApplyToState(nil, state.NewAppState(), nil); err == nil {
		t.Error("expected error for nil Config, got nil")
	}
	if err := ApplyToState(&Config{}, nil, nil); err == nil {
		t.Error("expected error for nil AppState, got nil")
	}
}

// parseWithConfigFile resolves a temporary apt-proxy.yaml holding body
// together with args through ParseFlagsWithConfigFile.
func parseWithConfigFile(t *testing.T, body string, args ...string) (*Config, error) {
	t.Helper()
	var (
		cfg *Config
		err error
	)
	withArgs(t, append([]string{"apt-proxy", "--config=" + writeConfigFile(t, body)}, args...), func() {
		cfg, err = ParseFlagsWithConfigFile()
	})
	return cfg, err
}

// TestConfigFileKeepsNonZeroDefaults covers the settings whose default is not
// the zero value: an omitted YAML key keeps the default, an explicit YAML
// 0/false is honored, and flags and environment variables still win.
func TestConfigFileKeepsNonZeroDefaults(t *testing.T) {
	const disabled = "security:\n  api_rate_limit_per_minute: 0\nstorage:\n  s3:\n    use_ssl: false\nmirrors:\n  election: false\n"
	for _, tc := range []struct {
		name          string
		yaml          string
		args          []string
		env           map[string]string
		wantRateLimit int
		wantUseSSL    bool
		wantElection  bool
	}{
		{name: "empty file", wantRateLimit: DefaultAPIRateLimitPerMinute, wantUseSSL: true, wantElection: true},
		{
			name:          "sections without the keys",
			yaml:          "security:\n  api_key: file-key\nstorage:\n  backend: s3\n  s3:\n    endpoint: s3.example.com\nmirrors:\n  debian: https://mirror.example/debian/\n",
			wantRateLimit: DefaultAPIRateLimitPerMinute,
			wantUseSSL:    true,
			wantElection:  true,
		},
		{name: "explicit zero and false", yaml: disabled, wantRateLimit: 0, wantUseSSL: false, wantElection: false},
		{
			name:          "flags override the file",
			yaml:          disabled,
			args:          []string{"--api-rate-limit=30", "--s3-use-ssl=true", "--mirror-election=true"},
			wantRateLimit: 30,
			wantUseSSL:    true,
			wantElection:  true,
		},
		{
			name:          "environment overrides the file",
			yaml:          "security:\n  api_rate_limit_per_minute: 30\nstorage:\n  s3:\n    use_ssl: true\nmirrors:\n  election: true\n",
			env:           map[string]string{EnvAPIRateLimitPerMinute: "0", EnvS3UseSSL: "false", EnvMirrorElection: "false"},
			wantRateLimit: 0,
			wantUseSSL:    false,
			wantElection:  false,
		},
		{
			name:          "flags override the environment",
			yaml:          disabled,
			args:          []string{"--mirror-election=false"},
			env:           map[string]string{EnvMirrorElection: "true"},
			wantRateLimit: 0,
			wantUseSSL:    false,
			wantElection:  false,
		},
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
			if got := cfg.Security.APIRateLimitPerMinute; got != tc.wantRateLimit {
				t.Errorf("APIRateLimitPerMinute = %d, want %d", got, tc.wantRateLimit)
			}
			if got := cfg.Storage.S3.UseSSL; got != tc.wantUseSSL {
				t.Errorf("S3.UseSSL = %v, want %v", got, tc.wantUseSSL)
			}
			if got := !cfg.Mirrors.DisableElection; got != tc.wantElection {
				t.Errorf("mirror election = %v, want %v", got, tc.wantElection)
			}
		})
	}
}

// TestEmptyAPIKeyKeepsFileAuthentication covers an API key flag or environment
// variable set to the empty string. It is not a key, so the key and the
// authentication enabled by the config file stay in place; only the explicit
// toggle turns authentication off.
func TestEmptyAPIKeyKeepsFileAuthentication(t *testing.T) {
	// The file enables authentication either explicitly or by the key alone.
	fileForms := []struct{ name, toggle string }{
		{"explicit toggle", "  enable_api_auth: true\n"},
		{"implied by key", ""},
	}
	for _, tc := range []struct {
		name        string
		args        []string
		env         map[string]string
		wantKey     string
		wantEnabled bool
	}{
		{name: "empty flag", args: []string{"--api-key="}, wantKey: "file-key", wantEnabled: true},
		{name: "empty environment variable", env: map[string]string{EnvAPIKey: ""}, wantKey: "file-key", wantEnabled: true},
		{name: "replacement key", args: []string{"--api-key=flag-key"}, wantKey: "flag-key", wantEnabled: true},
		{name: "explicit false flag", args: []string{"--enable-api-auth=false"}, wantKey: "file-key", wantEnabled: false},
		{name: "explicit false environment variable", env: map[string]string{EnvEnableAPIAuth: "false"}, wantKey: "file-key", wantEnabled: false},
		{name: "explicit false with an empty key", args: []string{"--api-key=", "--enable-api-auth=false"}, wantKey: "file-key", wantEnabled: false},
	} {
		for _, form := range fileForms {
			t.Run(tc.name+"/"+form.name, func(t *testing.T) {
				clearConfigEnv(t)
				for name, value := range tc.env {
					t.Setenv(name, value)
				}
				cfg, err := parseWithConfigFile(t, "security:\n  api_key: file-key\n"+form.toggle, tc.args...)
				if err != nil {
					t.Fatalf("ParseFlagsWithConfigFile: %v", err)
				}
				if cfg.Security.APIKey != tc.wantKey {
					t.Errorf("APIKey = %q, want %q", cfg.Security.APIKey, tc.wantKey)
				}
				if cfg.Security.EnableAPIAuth != tc.wantEnabled {
					t.Errorf("EnableAPIAuth = %v, want %v", cfg.Security.EnableAPIAuth, tc.wantEnabled)
				}
			})
		}
	}
}

// valueSettings holds the options that have no empty value: booleans, numbers
// and the mode. The mode is the comma-joined list, which keeps the struct
// comparable.
type valueSettings struct {
	Mode                  string
	Debug                 bool
	CacheMaxSize          int64
	CacheTTL              time.Duration
	CacheCleanupInterval  time.Duration
	TLSEnabled            bool
	EnableAPIAuth         bool
	APIRateLimitPerMinute int
	UpstreamKeepAlive     bool
	FollowRedirects       bool
	MirrorElection        bool
	S3UseSSL              bool
	S3UsePathStyle        bool
	S3InlineMaxMB         int64
}

func valueSettingsOf(cfg *Config) valueSettings {
	return valueSettings{
		Mode:                  strings.Join(cfg.Modes, ","),
		Debug:                 cfg.Debug,
		CacheMaxSize:          cfg.Cache.MaxSize,
		CacheTTL:              cfg.Cache.TTL,
		CacheCleanupInterval:  cfg.Cache.CleanupInterval,
		TLSEnabled:            cfg.TLS.Enabled,
		EnableAPIAuth:         cfg.Security.EnableAPIAuth,
		APIRateLimitPerMinute: cfg.Security.APIRateLimitPerMinute,
		UpstreamKeepAlive:     cfg.UpstreamKeepAlive,
		FollowRedirects:       cfg.PassthroughFollowRedirects,
		MirrorElection:        !cfg.Mirrors.DisableElection,
		S3UseSSL:              cfg.Storage.S3.UseSSL,
		S3UsePathStyle:        cfg.Storage.S3.UsePathStyle,
		S3InlineMaxMB:         cfg.Storage.S3.InlineMaxMB,
	}
}

// TestEmptyEnvironmentValueKeepsFileSetting covers environment variables that
// are set to the empty string for options without an empty value. The
// resolvers ignore such a variable, so the config file's setting must stay in
// place instead of being replaced by the option's default. A variable that
// holds a value still overrides the file.
func TestEmptyEnvironmentValueKeepsFileSetting(t *testing.T) {
	// Every setting differs from its default.
	const file = `mode: debian
upstream_keep_alive: false
passthrough_follow_redirects: false
server:
  debug: true
cache:
  max_size_gb: 4
  ttl_hours: 12
  cleanup_interval_min: 7
mirrors:
  ubuntu: https://mirror.example/ubuntu/
  election: false
tls:
  enabled: true
security:
  api_key: file-key
  api_rate_limit_per_minute: 30
storage:
  s3:
    use_ssl: false
    use_path_style: true
    inline_max_mb: 3
`
	fromFile := valueSettings{
		Mode:                  "debian",
		Debug:                 true,
		CacheMaxSize:          4 << 30,
		CacheTTL:              12 * time.Hour,
		CacheCleanupInterval:  7 * time.Minute,
		TLSEnabled:            true,
		EnableAPIAuth:         true,
		APIRateLimitPerMinute: 30,
		UpstreamKeepAlive:     false,
		FollowRedirects:       false,
		MirrorElection:        false,
		S3UseSSL:              false,
		S3UsePathStyle:        true,
		S3InlineMaxMB:         3,
	}
	overrides := map[string]string{
		EnvMode:                  "ubuntu",
		EnvDebug:                 "false",
		EnvCacheMaxSize:          "9",
		EnvCacheTTL:              "5",
		EnvCacheCleanupInterval:  "0",
		EnvTLSEnabled:            "false",
		EnvEnableAPIAuth:         "false",
		EnvAPIRateLimitPerMinute: "0",
		EnvUpstreamKeepAlive:     "true",
		EnvMirrorElection:        "true",
		EnvS3UseSSL:              "true",
		EnvS3UsePathStyle:        "false",
		EnvS3InlineMaxMB:         "8",

		EnvPassthroughFollowRedirects: "true",
	}
	fromEnvironment := valueSettings{
		Mode:                  "ubuntu",
		Debug:                 false,
		CacheMaxSize:          9 << 30,
		CacheTTL:              5 * time.Hour,
		CacheCleanupInterval:  0,
		TLSEnabled:            false,
		EnableAPIAuth:         false,
		APIRateLimitPerMinute: 0,
		UpstreamKeepAlive:     true,
		FollowRedirects:       true,
		MirrorElection:        true,
		S3UseSSL:              true,
		S3UsePathStyle:        false,
		S3InlineMaxMB:         8,
	}
	withValue := func(value string) map[string]string {
		env := make(map[string]string, len(overrides))
		for name := range overrides {
			env[name] = value
		}
		return env
	}

	for _, tc := range []struct {
		name string
		env  map[string]string
		want valueSettings
	}{
		{name: "unset", want: fromFile},
		{name: "empty", env: withValue(""), want: fromFile},
		{name: "blank", env: withValue(" "), want: fromFile},
		{name: "values", env: overrides, want: fromEnvironment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			cfg, err := parseWithConfigFile(t, file)
			if err != nil {
				t.Fatalf("ParseFlagsWithConfigFile: %v", err)
			}
			if got := valueSettingsOf(cfg); got != tc.want {
				t.Errorf("settings:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}

	// Without a file setting the defaults apply. An empty variable used to
	// count as an explicit zero for the cache options, which turned off the
	// size limit and expiry.
	t.Run("empty without a file setting", func(t *testing.T) {
		clearConfigEnv(t)
		defaults, err := parseWithConfigFile(t, "")
		if err != nil {
			t.Fatalf("ParseFlagsWithConfigFile: %v", err)
		}
		for name := range overrides {
			t.Setenv(name, "")
		}
		cfg, err := parseWithConfigFile(t, "")
		if err != nil {
			t.Fatalf("ParseFlagsWithConfigFile: %v", err)
		}
		got, want := valueSettingsOf(cfg), valueSettingsOf(defaults)
		if got != want {
			t.Errorf("settings:\n got %+v\nwant %+v", got, want)
		}
		if got.CacheMaxSize == 0 || got.CacheTTL == 0 || got.CacheCleanupInterval == 0 {
			t.Errorf("cache limits are off: %+v", got)
		}
	})

	// String options are unchanged: the presence of the variable still counts,
	// so an empty one clears the mirror the file selects.
	t.Run("empty string option", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv(EnvUbuntu, "")
		cfg, err := parseWithConfigFile(t, file)
		if err != nil {
			t.Fatalf("ParseFlagsWithConfigFile: %v", err)
		}
		if cfg.Mirrors.Ubuntu != "" {
			t.Errorf("Mirrors.Ubuntu = %q, want it cleared", cfg.Mirrors.Ubuntu)
		}
	})
}
