package define_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	define "github.com/soulteary/apt-proxy/define"
)

func TestRuleToString(t *testing.T) {
	r := define.Rule{
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
	if define.GenerateAliasFromURL("http://mirrors.cn99.com/ubuntu/") != "cn:cn99" {
		t.Fatal("generate alias from url failed")
	}

	if define.GenerateAliasFromURL("https://mirrors.tuna.tsinghua.edu.cn/ubuntu/") != "cn:tsinghua" {
		t.Fatal("generate alias from url failed")
	}

	if define.GenerateAliasFromURL("mirrors.cnnic.cn/ubuntu/") != "cn:cnnic" {
		t.Fatal("generate alias from url failed")
	}
}

func TestGenerateBuildInMirorItem(t *testing.T) {
	mirror := define.GenerateBuildInMirorItem("http://mirrors.tuna.tsinghua.edu.cn/ubuntu/", true)
	if !(mirror.Http == true && mirror.Https == false) || mirror.Official != true {
		t.Fatal("generate build-in mirror item failed")
	}
	mirror = define.GenerateBuildInMirorItem("https://mirrors.tuna.tsinghua.edu.cn/ubuntu/", false)
	if !(mirror.Http == false && mirror.Https == true) || mirror.Official != false {
		t.Fatal("generate build-in mirror item failed")
	}
}

func TestGenerateBuildInList(t *testing.T) {
	mirrors := define.GenerateBuildInList(define.UBUNTU_OFFICIAL_MIRRORS, define.UBUNTU_CUSTOM_MIRRORS)

	count := 0
	for _, url := range define.UBUNTU_OFFICIAL_MIRRORS {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			count += 1
		} else {
			count += 2
		}
	}
	for _, url := range define.UBUNTU_CUSTOM_MIRRORS {
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
