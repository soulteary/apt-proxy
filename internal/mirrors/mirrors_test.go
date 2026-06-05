package mirrors

import (
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestGetUbuntuMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(distro.TypeUbuntu, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/ubuntu/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(distro.TypeUbuntu, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetDebianMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(distro.TypeDebian, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/debian/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(distro.TypeDebian, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetCentOSMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(distro.TypeCentOS, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/centos/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(distro.TypeCentOS, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetMirrorUrlsByGeo(t *testing.T) {
	mirrors := GetGeoMirrorUrlsByMode(distro.TypeAllDistros)
	if len(mirrors) == 0 {
		t.Fatal("No mirrors found")
	}

	mirrors = GetGeoMirrorUrlsByMode(distro.TypeDebian)
	if len(mirrors) != len(distro.BuiltinDebianMirrors) {
		t.Fatal("Get mirrors error")
	}

	mirrors = GetGeoMirrorUrlsByMode(distro.TypeUbuntu)
	if len(mirrors) == 0 {
		t.Fatal("No mirrors found")
	}
}

func TestGetPredefinedConfiguration(t *testing.T) {
	res, pattern := GetPredefinedConfiguration(distro.TypeUbuntu)
	if res != distro.UbuntuBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}
	if !pattern.MatchString("/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(distro.TypeDebian)
	if res != distro.DebianBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/debian/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(distro.TypeCentOS)
	if res != distro.CentosBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/centos/test/repomd.xml") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(distro.TypeAlpine)
	if res != distro.AlpineBenchmarkURL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/alpine/test/APKINDEX.tar.gz") {
		t.Fatal("Failed to verify domain name rules")
	}
}
