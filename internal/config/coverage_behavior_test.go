package config

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
)

// All legacy fields must survive a fully populated override, while an absent
// override must leave the original configuration intact.
func TestMergeCompleteOperatorOverride(t *testing.T) {
	override := &Config{
		Debug: true, CacheDir: "operator-cache", Mode: distro.TypeDebian, Listen: "127.0.0.1:1234", UpstreamKeepAlive: true,
		Mirrors:     MirrorConfig{Ubuntu: "u", UbuntuPorts: "up", Debian: "d", DebianSecurity: "ds", CentOS: "c", Alpine: "a"},
		Cache:       CacheConfig{MaxSize: 1024, TTL: time.Hour, CleanupInterval: time.Minute, Integrity: "sha256"},
		TLS:         TLSConfig{Enabled: true, CertFile: "cert", KeyFile: "key"},
		Security:    SecurityConfig{APIKey: "key", EnableAPIAuth: true, APIRateLimitPerMinute: 12, TrustedProxies: []string{"127.0.0.1/32"}},
		Storage:     StorageConfig{Backend: "s3", S3: S3Config{Endpoint: "store:9000", Region: "region", Bucket: "bucket", Prefix: "prefix", AccessKey: "access", SecretKey: "secret", SessionToken: "token", UseSSL: true, UsePathStyle: true, InlineMaxMB: 5, TempDir: "temp"}},
		Resources:   ResourceConfig{MaxObjectMB: 1, TotalTempMB: 2, MaxActiveDownloads: 3, MinFreeMB: 4},
		Passthrough: []string{"example.com"}, DistributionsConfigPath: "dist.yaml", MirrorHealth: &mirrorhealth.Config{},
	}
	ex := &cliExplicit{}
	mask := reflect.ValueOf(ex).Elem()
	for i := 0; i < mask.NumField(); i++ {
		mask.Field(i).SetBool(true)
	}
	for _, explicit := range []*cliExplicit{nil, ex} {
		got := MergeConfigsWithExplicit(&Config{}, override, explicit)
		if !reflect.DeepEqual(got, override) {
			t.Fatalf("explicit=%v merge differs:\ngot %#v\nwant %#v", explicit != nil, got, override)
		}
		if !reflect.DeepEqual(MergeConfigsWithExplicit(got, &Config{}, &cliExplicit{}), got) {
			t.Fatal("implicit zero override changed operator values")
		}
		got.Security.TrustedProxies[0] = "changed"
		if override.Security.TrustedProxies[0] != "127.0.0.1/32" {
			t.Fatal("merged trusted proxies aliases override")
		}
	}
}

func TestExplicitOverridesCanDisableLimitsAndFeatures(t *testing.T) {
	base := &Config{Debug: true, Mode: 3, UpstreamKeepAlive: true, Cache: CacheConfig{MaxSize: 1, TTL: time.Hour, CleanupInterval: time.Minute}, TLS: TLSConfig{Enabled: true}, Security: SecurityConfig{EnableAPIAuth: true, APIRateLimitPerMinute: 5, TrustedProxies: []string{"127.0.0.1"}}, Storage: StorageConfig{S3: S3Config{UseSSL: true, UsePathStyle: true, InlineMaxMB: 5}}, Passthrough: []string{"example.com"}}
	ex := &cliExplicit{Debug: true, Mode: true, UpstreamKeepAlive: true, CacheMaxSize: true, CacheTTL: true, CacheCleanupInterval: true, TLSEnabled: true, EnableAPIAuth: true, APIRateLimitPerMinute: true, TrustedProxies: true, Passthrough: true, S3UseSSL: true, S3UsePathStyle: true, S3InlineMaxMB: true}
	got := MergeConfigsWithExplicit(base, &Config{}, ex)
	if !reflect.DeepEqual(got, &Config{}) {
		t.Fatalf("explicit zero values not honored: %#v", got)
	}
	if !base.Debug || base.Cache.TTL != time.Hour {
		t.Fatal("merge mutated base")
	}
}

func TestGroupedHelpKeepsEveryFlagVisible(t *testing.T) {
	for _, name := range []string{"", "custom-name"} {
		fs := flag.NewFlagSet(name, flag.ContinueOnError)
		var out bytes.Buffer
		fs.SetOutput(&out)
		defineFlags(fs)
		fs.String("future-option", "present", "future `value`")
		fs.String("empty-help", "value", "")
		fs.String("empty-default", "", "")
		installGroupedUsage(fs)
		fs.Usage()
		fs.VisitAll(func(f *flag.Flag) {
			if strings.Count(out.String(), "  -"+f.Name+" ")+strings.Count(out.String(), "  -"+f.Name+"\n") != 1 {
				t.Errorf("flag %s missing or duplicated", f.Name)
			}
		})
		if !strings.Contains(out.String(), "# Other") || !strings.Contains(out.String(), "future value") && !strings.Contains(out.String(), "future `value`") {
			t.Fatalf("lost ungrouped flag/help: %s", out.String())
		}
	}
	fs := flag.NewFlagSet("partial", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	installGroupedUsage(fs)
	fs.Usage()
}

func TestFlagParsingReportsErrorsAndAutoDiscoversFile(t *testing.T) {
	clearConfigEnv(t)
	for _, parse := range []func() (*Config, error){ParseFlags, ParseFlagsWithConfigFile} {
		withArgs(t, []string{"apt-proxy", "--unknown-option"}, func() {
			if _, err := parse(); err == nil {
				t.Fatal("unknown flag accepted")
			}
		})
	}

	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("server: ["), 0600); err != nil {
		t.Fatal(err)
	}
	withArgs(t, []string{"apt-proxy", "--config=" + path}, func() {
		if _, err := ParseFlagsWithConfigFile(); err == nil {
			t.Fatal("invalid YAML accepted")
		}
	})
	t.Chdir(t.TempDir())
	if err := os.WriteFile(DefaultConfigFileName, []byte("server:\n  port: '1234'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	withArgs(t, []string{"apt-proxy"}, func() {
		got, err := ParseFlagsWithConfigFile()
		if err != nil || !strings.HasSuffix(got.Listen, ":1234") {
			t.Fatalf("discovery: %v %v", got, err)
		}
	})
	if _, err := LoadConfigFile(t.TempDir()); err == nil {
		t.Fatal("directory accepted as YAML file")
	}
}

func TestBuildCLIUsesDefaultForMissingAddressHalf(t *testing.T) {
	clearConfigEnv(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--port=1234"}, "localhost:1234"},
		{[]string{"--host=127.0.0.2"}, "127.0.0.2:4321"},
	} {
		fs := flag.NewFlagSet("", flag.ContinueOnError)
		defineFlags(fs)
		// Empty registered defaults expose the two supported partial-address cases.
		if err := fs.Set("host", ""); err != nil {
			t.Fatal(err)
		}
		if err := fs.Set("port", ""); err != nil {
			t.Fatal(err)
		}
		if err := fs.Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		got, _ := buildCLIConfig(fs, "localhost", "4321", "", 0, 0, 0)
		if got.Listen != tc.want {
			t.Fatalf("unexpected listen %s", got.Listen)
		}
	}
	for name, want := range map[string]int{"ubuntu-ports": 2, "centos": 4, "alpine": 5} {
		if got := ModeToInt(name); got != want {
			t.Fatalf("%s: %d", name, got)
		}
	}
	t.Setenv(EnvAPIKey, "key")
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	defineFlags(fs)
	got, ex := buildCLIConfig(fs, "", "", "", 0, 0, 0)
	if !got.Security.EnableAPIAuth || !ex.APIKey {
		t.Fatal("ENV API key did not enable auth")
	}
	if got := splitCommaList(" a, ,b ,"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("list: %v", got)
	}
}

func TestValidateTLSFilesAndBackendFailures(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert")
	key := filepath.Join(dir, "key")
	for _, p := range []string{cert, key} {
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		tls  TLSConfig
		want string
	}{
		{"missing cert", TLSConfig{Enabled: true}, "certificate file must"},
		{"missing key", TLSConfig{Enabled: true, CertFile: cert}, "key file must"},
		{"absent cert", TLSConfig{Enabled: true, CertFile: cert + ".missing", KeyFile: key}, "certificate file not found"},
		{"absent key", TLSConfig{Enabled: true, CertFile: cert, KeyFile: key + ".missing"}, "key file not found"},
		{"files present", TLSConfig{Enabled: true, CertFile: cert, KeyFile: key}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Listen: ":3142", CacheDir: dir, TLS: tc.tls}
			err := ValidateConfig(cfg)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
	// A directory at the write-probe filename fails even for a privileged test user.
	if err := os.Mkdir(filepath.Join(dir, ".apt-proxy-write-test"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(&Config{Listen: ":3142", CacheDir: dir}); err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("write probe: %v", err)
	}
	if err := ValidateConfig(&Config{MirrorHealth: &mirrorhealth.Config{FailureThreshold: -1}}); err == nil {
		t.Fatal("invalid health configuration accepted")
	}
	if err := ValidateConfig(&Config{Resources: ResourceConfig{MaxObjectMB: -1}}); err == nil {
		t.Fatal("invalid resource configuration accepted")
	}
	if err := ValidateConfig(&Config{Cache: CacheConfig{Integrity: "unknown"}}); err == nil {
		t.Fatal("invalid integrity accepted")
	}
	if err := ValidateConfig(&Config{SharedCache: &sharedcache.Config{}}); err == nil {
		t.Fatal("invalid shared cache accepted")
	}
	shared := &sharedcache.Config{Repositories: []sharedcache.Repository{{ID: "debian", Bases: []string{"https://deb.debian.org/debian/"}}}, CoordinationDir: cert}
	if err := ValidateConfig(&Config{SharedCache: shared}); err == nil {
		t.Fatal("coordination path file accepted")
	}
	shared.CoordinationDir = filepath.Join(t.TempDir(), "coord")
	if err := ValidateConfig(&Config{Listen: ":3142", CacheDir: t.TempDir(), SharedCache: shared}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(&Config{Listen: ":3142", CacheDir: "ignored", Storage: StorageConfig{Backend: "s3", S3: S3Config{Endpoint: "store:9000", Bucket: "bucket"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestFindConfigFallsBackFromMissingEnvironmentPath(t *testing.T) {
	clearConfigEnv(t)
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvConfigFile, "missing-config.yaml")
	// A system configuration may exist; the local file must still take priority.
	if err := os.WriteFile(DefaultConfigFileName, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := FindConfigFile(); got != DefaultConfigFileName {
		t.Fatalf("current directory config %s", got)
	}
}

func TestYAMLExplicitKeepAliveAndUnresolvedEnvironment(t *testing.T) {
	t.Setenv("APT_PROXY_TEST_UNUSED", "value")
	if err := os.Unsetenv("APT_PROXY_TEST_UNUSED"); err != nil {
		t.Fatal(err)
	}
	if got := expandConfigEnv("${APT_PROXY_TEST_UNUSED}"); got != "${APT_PROXY_TEST_UNUSED}" {
		t.Fatalf("unresolved variable lost: %s", got)
	}
	disabled := false
	cfg := yamlConfigToConfig(&YAMLConfig{UpstreamKeepAlive: &disabled})
	if cfg.UpstreamKeepAlive {
		t.Fatal("YAML explicit false ignored")
	}
}
