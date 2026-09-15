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

package distro

import "regexp"

const (
	DebianBenchmarkURL = "dists/bullseye/main/binary-amd64/Release"
)

var DebianHostPattern = regexp.MustCompile(`/debian(-security)?/(.+)$`)

// DebianSecurityHostPattern matches requests addressed to the Debian security
// archive host. The classic sources.list form
//
//	deb http://security.debian.org <suite>-security main
//
// requests paths like /dists/trixie-security/InRelease, with no
// /debian-security/ prefix for DebianHostPattern to latch onto, so these
// requests are recognised by Host instead. An optional :port is tolerated
// because Host carries whatever the client dialled.
//
// Host patterns are matched against a lower-cased host (DNS names are
// case-insensitive, so Security.Debian.Org is the same archive), hence the
// lower-case literal here.
var DebianSecurityHostPattern = regexp.MustCompile(`^security\.debian\.org(:\d+)?$`)

// BuiltinHostPattern returns the compile-time Host matcher for a distro type,
// or nil when it has none. LoadFromConfig falls back to it so a
// distributions.yaml entry that omits host_pattern keeps the built-in
// behaviour, the same way omitting mirrors keeps the built-in mirror list.
func BuiltinHostPattern(distType int) *regexp.Regexp {
	if distType == TypeDebian {
		return DebianSecurityHostPattern
	}
	return nil
}

// https://www.debian.org/mirror/list 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var DebianOfficialMirrors = []string{
	"http://ftp.cn.debian.org/debian/",
	"mirror.bjtu.edu.cn/debian/",
	"mirrors.163.com/debian/",
	"mirrors.bfsu.edu.cn/debian/",
	"mirrors.huaweicloud.com/debian/",
	"http://mirrors.neusoft.edu.cn/debian/",
	"mirrors.tuna.tsinghua.edu.cn/debian/",
	"mirrors.ustc.edu.cn/debian/",
}

var DebianCustomMirrors = []string{
	"repo.huaweicloud.com/debian/",
	"mirrors.cloud.tencent.com/debian/",
	"mirrors.hit.edu.cn/debian/",
	"mirrors.aliyun.com/debian/",
	"mirror.lzu.edu.cn/debian/",
	"mirror.nju.edu.cn/debian/",
}

var BuiltinDebianMirrors = GenerateBuildInList(DebianOfficialMirrors, DebianCustomMirrors)

var DebianDefaultCacheRules = newDebStyleRules(TypeDebian)
