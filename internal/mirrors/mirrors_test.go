package mirrors

import (
	"strings"
	"testing"

	Define "github.com/soulteary/apt-proxy/define"
)

func TestGetUbuntuMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_UBUNTU, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/ubuntu/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_UBUNTU, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetDebianMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_DEBIAN, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/debian/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_DEBIAN, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetCentOSMirrorByAliases(t *testing.T) {
	alias := GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_CENTOS, "cn:tsinghua")
	if !strings.Contains(alias, "mirrors.tuna.tsinghua.edu.cn/centos/") {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}

	alias = GetMirrorURLByAliases(Define.TYPE_LINUX_DISTROS_CENTOS, "cn:not-found")
	if alias != "" {
		t.Fatal("Test Get Mirror By Custom Name Failed")
	}
}

func TestGetMirrorUrlsByGeo(t *testing.T) {
	mirrors := GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_ALL_DISTROS)
	if len(mirrors) == 0 {
		t.Fatal("No mirrors found")
	}

	mirrors = GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_DEBIAN)
	if len(mirrors) != len(BUILDIN_DEBIAN_MIRRORS) {
		t.Fatal("Get mirrors error")
	}

	mirrors = GetGeoMirrorUrlsByMode(Define.TYPE_LINUX_DISTROS_UBUNTU)
	if len(mirrors) == 0 {
		t.Fatal("No mirrors found")
	}
}

func TestGetPredefinedConfiguration(t *testing.T) {
	res, pattern := GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_UBUNTU)
	if res != Define.UBUNTU_BENCHMAKR_URL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}
	if !pattern.MatchString("/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_DEBIAN)
	if res != Define.DEBIAN_BENCHMAKR_URL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/debian/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_CENTOS)
	if res != Define.CENTOS_BENCHMAKR_URL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/centos/test/repomd.xml") {
		t.Fatal("Failed to verify domain name rules")
	}

	res, pattern = GetPredefinedConfiguration(Define.TYPE_LINUX_DISTROS_ALPINE)
	if res != Define.ALPINE_BENCHMAKR_URL {
		t.Fatal("Failed to get resource link")
	}
	if !pattern.MatchString("/alpine/test/APKINDEX.tar.gz") {
		t.Fatal("Failed to verify domain name rules")
	}
}
