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

import (
	"regexp"
)

const (
	UbuntuPortsGeoMirrorAPI = "http://mirrors.ubuntu.com/mirrors.txt"
	// dists/devel aliases the current development series, so unlike a codename
	// the path survives every release reaching end of life. The suite-level
	// Release file is the same for all architectures.
	UbuntuPortsBenchmarkURL = "dists/devel/Release"
)

var UbuntuPortsHostPattern = regexp.MustCompile(`/ubuntu-ports/(.+)$`)

// Based on http://mirrors.ubuntu.com/mirrors.txt 2022.11.19. That list covers
// the main archive, so hosts that do not carry ubuntu-ports are left out.
// Sites that contain protocol headers, restrict access to resources using that protocol
var UbuntuPortsOfficialMirrors = []string{
	"mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
	"mirrors.cnnic.cn/ubuntu-ports/",
	"mirror.bjtu.edu.cn/ubuntu-ports/",
	"mirrors.cqu.edu.cn/ubuntu-ports/",
	"http://mirrors.skyshe.cn/ubuntu-ports/",
	"mirrors.yun-idc.com/ubuntu-ports/",
	"http://mirror.dlut.edu.cn/ubuntu-ports/",
	"mirrors.huaweicloud.com/repository/ubuntu-ports/",
	"mirrors.bupt.edu.cn/ubuntu-ports/",
	"mirrors.hit.edu.cn/ubuntu-ports/",
	"mirror.nju.edu.cn/ubuntu-ports/",
	"mirrors.bfsu.edu.cn/ubuntu-ports/",
	"mirror.lzu.edu.cn/ubuntu-ports/",
	"mirrors.aliyun.com/ubuntu-ports/",
	"mirrors.njupt.edu.cn/ubuntu-ports/",
	"mirrors.cloud.tencent.com/ubuntu-ports/",
	"mirrors.ustc.edu.cn/ubuntu-ports/",
	"mirrors.sdu.edu.cn/ubuntu-ports/",
}

var UbuntuPortsCustomMirrors = []string{
	"mirrors.163.com/ubuntu-ports/",
}

var BuiltinUbuntuPortsMirrors = GenerateBuildInList(UbuntuPortsOfficialMirrors, UbuntuPortsCustomMirrors)

var UbuntuPortsDefaultCacheRules = newDebStyleRules(TypeUbuntuPorts)
