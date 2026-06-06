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
	if (mirror.HTTP() != true || mirror.HTTPS() != false) || mirror.Official != true {
		t.Fatal("generate build-in mirror item failed")
	}
	mirror = distro.GenerateBuildInMirorItem("https://mirrors.tuna.tsinghua.edu.cn/ubuntu/", false)
	if (mirror.HTTP() != false || mirror.HTTPS() != true) || mirror.Official != false {
		t.Fatal("generate build-in mirror item failed")
	}
}

func TestGenerateBuildInList(t *testing.T) {
	mirrors := distro.GenerateBuildInList(distro.UbuntuOfficialMirrors, distro.UbuntuCustomMirrors)

	count := 0
	for _, url := range distro.UbuntuOfficialMirrors {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			count++
		} else {
			count += 2
		}
	}
	for _, url := range distro.UbuntuCustomMirrors {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			count++
		} else {
			count += 2
		}
	}

	if len(mirrors) != count {
		t.Fatal("generate build-in mirror list failed")
	}
}

func TestReloadDistributionsConfig_NonExistentPath(t *testing.T) {
	// Reload with non-existent path: must return an error and leave the
	// previously-registered (built-in) distributions intact.
	err := distro.ReloadDistributionsConfig("/nonexistent/distributions.yaml")
	if err == nil {
		t.Fatal("expected error reloading from non-existent path, got nil")
	}
	reg := distro.GetRegistry()
	for _, id := range []string{distro.DistroUbuntu, distro.DistroDebian, distro.DistroCentOS, distro.DistroAlpine} {
		if _, ok := reg.GetByID(id); !ok {
			t.Errorf("expected built-in distribution %q to be registered", id)
		}
	}
	m := distro.GetHostPatternMap()
	if len(m) == 0 {
		t.Error("GetHostPatternMap() should return non-empty map after built-in registration")
	}
}
