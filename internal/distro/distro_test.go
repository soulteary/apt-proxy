package distro_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestRuleToString(t *testing.T) {
	r := distro.Rule{
		Pattern:      regexp.MustCompile(`a$`),
		CacheControl: "1",
		Rewrite:      true,
	}

	expect := fmt.Sprintf("%s Cache-Control=%s Rewrite=%#v", r.Pattern.String(), r.CacheControl, r.Rewrite)
	if expect != r.String() {
		t.Fatal("parse rule to string failed")
	}
}

func TestGenerateAliasFromURL(t *testing.T) {
	if distro.GenerateAliasFromURL("http://mirrors.cn99.com/ubuntu/") != "cn:cn99" {
		t.Fatal("generate alias from url failed")
	}

	if distro.GenerateAliasFromURL("https://mirrors.tuna.tsinghua.edu.cn/ubuntu/") != "cn:tsinghua" {
		t.Fatal("generate alias from url failed")
	}

	if distro.GenerateAliasFromURL("mirrors.cnnic.cn/ubuntu/") != "cn:cnnic" {
		t.Fatal("generate alias from url failed")
	}
}

func TestGenerateBuildInMirorItem(t *testing.T) {
	mirror := distro.GenerateBuildInMirorItem("http://mirrors.tuna.tsinghua.edu.cn/ubuntu/", true)
	if (mirror.Http != true || mirror.Https != false) || mirror.Official != true {
		t.Fatal("generate build-in mirror item failed")
	}
	mirror = distro.GenerateBuildInMirorItem("https://mirrors.tuna.tsinghua.edu.cn/ubuntu/", false)
	if (mirror.Http != false || mirror.Https != true) || mirror.Official != false {
		t.Fatal("generate build-in mirror item failed")
	}
}

func TestGenerateBuildInList(t *testing.T) {
	mirrors := distro.GenerateBuildInList(distro.UBUNTU_OFFICIAL_MIRRORS, distro.UBUNTU_CUSTOM_MIRRORS)

	count := 0
	for _, url := range distro.UBUNTU_OFFICIAL_MIRRORS {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			count += 1
		} else {
			count += 2
		}
	}
	for _, url := range distro.UBUNTU_CUSTOM_MIRRORS {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			count += 1
		} else {
			count += 2
		}
	}

	if len(mirrors) != count {
		t.Fatal("generate build-in mirror list failed")
	}
}

func TestReloadDistributionsConfig_NonExistentPath(t *testing.T) {
	// Reload with non-existent path: registry should keep only built-in distributions
	distro.ReloadDistributionsConfig("/nonexistent/distributions.yaml")
	reg := distro.GetRegistry()
	for _, id := range []string{distro.LINUX_DISTROS_UBUNTU, distro.LINUX_DISTROS_DEBIAN, distro.LINUX_DISTROS_CENTOS, distro.LINUX_DISTROS_ALPINE} {
		if _, ok := reg.GetByID(id); !ok {
			t.Errorf("expected built-in distribution %q to be registered", id)
		}
	}
	m := distro.GetHostPatternMap()
	if len(m) == 0 {
		t.Error("GetHostPatternMap() should return non-empty map after built-in registration")
	}
}
