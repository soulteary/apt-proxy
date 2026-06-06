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
	UbuntuGeoMirrorAPI = "http://mirrors.ubuntu.com/mirrors.txt"
	UbuntuBenchmarkURL = "dists/noble/main/binary-amd64/Release"
)

var UbuntuHostPattern = regexp.MustCompile(`/ubuntu/(.+)$`)

// http://mirrors.ubuntu.com/mirrors.txt 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var UbuntuOfficialMirrors = []string{
	"mirrors.cn99.com/ubuntu/",
	"mirrors.tuna.tsinghua.edu.cn/ubuntu/",
	"mirrors.cnnic.cn/ubuntu/",
	"mirror.bjtu.edu.cn/ubuntu/",
	"mirrors.cqu.edu.cn/ubuntu/",
	"http://mirrors.skyshe.cn/ubuntu/",
	"mirrors.yun-idc.com/ubuntu/",
	"http://mirror.dlut.edu.cn/ubuntu/",
	"mirrors.xjtu.edu.cn/ubuntu/",
	"mirrors.huaweicloud.com/repository/ubuntu/",
	"mirrors.bupt.edu.cn/ubuntu/",
	"mirrors.hit.edu.cn/ubuntu/",
	"http://mirrors.sohu.com/ubuntu/",
	"mirror.nju.edu.cn/ubuntu/",
	"mirrors.bfsu.edu.cn/ubuntu/",
	"mirror.lzu.edu.cn/ubuntu/",
	"mirrors.aliyun.com/ubuntu/",
	"ftp.sjtu.edu.cn/ubuntu/",
	"mirrors.njupt.edu.cn/ubuntu/",
	"mirrors.cloud.tencent.com/ubuntu/",
	"http://mirrors.dgut.edu.cn/ubuntu/",
	"mirrors.ustc.edu.cn/ubuntu/",
	"mirrors.sdu.edu.cn/ubuntu/",
	"http://cn.archive.ubuntu.com/ubuntu/",
}

var UbuntuCustomMirrors = []string{
	"mirrors.163.com/ubuntu/",
}

var BuiltinUbuntuMirrors = GenerateBuildInList(UbuntuOfficialMirrors, UbuntuCustomMirrors)

var UbuntuDefaultCacheRules = newDebStyleRules(TypeUbuntu)
