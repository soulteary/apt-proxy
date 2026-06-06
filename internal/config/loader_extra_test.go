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
	"strings"
	"testing"

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
		EnvDebug, EnvUbuntu, EnvUbuntuPorts, EnvDebian,
		EnvCentOS, EnvAlpine,
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
		if cfg.Mode != distro.TypeAllDistros {
			t.Errorf("Mode = %d, want TypeAllDistros (%d)", cfg.Mode, distro.TypeAllDistros)
		}
		if cfg.Listen == "" {
			t.Error("Listen should default to host:port, got empty")
		}
		if cfg.Cache.MaxSize == 0 {
			t.Error("Cache.MaxSize should be defaulted, got 0")
		}
	})
}

// TestParseFlagsModeOverride drives ParseFlags with --mode=ubuntu and
// asserts the validated mode is preserved through the resolver.
func TestParseFlagsModeOverride(t *testing.T) {
	clearConfigEnv(t)
	withArgs(t, []string{"apt-proxy", "--mode=ubuntu", "--port=12345"}, func() {
		cfg, err := ParseFlags()
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if cfg.Mode != distro.TypeUbuntu {
			t.Errorf("Mode = %d, want TypeUbuntu (%d)", cfg.Mode, distro.TypeUbuntu)
		}
		if !strings.HasSuffix(cfg.Listen, ":12345") {
			t.Errorf("Listen = %q, want suffix :12345", cfg.Listen)
		}
	})
}

// TestParseFlagsInvalidModeFallsBackToDefault documents the current
// (cli-kit) fallback behaviour: an invalid CLI/ENV value is silently
// dropped and the default mode is used. This pins the contract so a
// future cli-kit upgrade that switches to hard-failing on invalid
// enums would surface as an explicit, conscious change here.
func TestParseFlagsInvalidModeFallsBackToDefault(t *testing.T) {
	clearConfigEnv(t)
	withArgs(t, []string{"apt-proxy", "--mode=fakedistro"}, func() {
		cfg, err := ParseFlags()
		if err != nil {
			t.Fatalf("ParseFlags returned error %v; cli-kit currently silently falls back instead", err)
		}
		if cfg.Mode != distro.TypeAllDistros {
			t.Errorf("Mode = %d, want fallback to TypeAllDistros (%d)",
				cfg.Mode, distro.TypeAllDistros)
		}
	})
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
		if cfg.Mode != distro.TypeUbuntu {
			t.Errorf("Mode = %d, want TypeUbuntu", cfg.Mode)
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
	base := &Config{Debug: true, UpstreamKeepAlive: true}
	override := &Config{Debug: false, UpstreamKeepAlive: false}
	ex := &cliExplicit{Debug: true, UpstreamKeepAlive: true}

	got := MergeConfigsWithExplicit(base, override, ex)
	if got.Debug {
		t.Error("Debug should have been overridden to false")
	}
	if got.UpstreamKeepAlive {
		t.Error("UpstreamKeepAlive should have been overridden to false")
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
	if st.GetProxyMode() != distro.TypeUbuntu {
		t.Errorf("ProxyMode = %d, want TypeUbuntu", st.GetProxyMode())
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

// TestGetAllowedModes pins the public list returned to CLI/help output.
// It must contain every distro the proxy actually knows how to mirror;
// drift here would silently break --mode validation.
func TestGetAllowedModes(t *testing.T) {
	got := GetAllowedModes()
	want := []string{
		distro.DistroAll,
		distro.DistroUbuntu,
		distro.DistroUbuntuPorts,
		distro.DistroDebian,
		distro.DistroCentOS,
		distro.DistroAlpine,
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i, m := range want {
		if got[i] != m {
			t.Errorf("[%d] = %q, want %q", i, got[i], m)
		}
	}
}

// TestModeToInt covers the default fallback branch for unknown modes,
// previously unexercised. The other branches are hit by the parse tests.
func TestModeToIntUnknown(t *testing.T) {
	if got := ModeToInt("not-a-mode"); got != distro.TypeAllDistros {
		t.Errorf("ModeToInt(unknown) = %d, want TypeAllDistros (%d)", got, distro.TypeAllDistros)
	}
}
