package distro

import (
	"regexp"
)

const (
	UbuntuPortsGeoMirrorAPI = "http://mirrors.ubuntu.com/mirrors.txt"
	// Ubuntu Ports targets non-amd64 architectures. The previous
	// `dists/noble/InRelease/Release` value pointed to a path that does not
	// exist (InRelease is a file, not a directory) and made every benchmark
	// 404. Use the arm64 Release file which mirrors universally publish.
	UbuntuPortsBenchmarkURL = "dists/noble/main/binary-arm64/Release"
)

var UbuntuPortsHostPattern = regexp.MustCompile(`/ubuntu-ports/(.+)$`)

// http://mirrors.ubuntu.com/mirrors.txt 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var UbuntuPortsOfficialMirrors = []string{
	"mirrors.cn99.com/ubuntu-ports/",
	"mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
	"mirrors.cnnic.cn/ubuntu-ports/",
	"mirror.bjtu.edu.cn/ubuntu-ports/",
	"mirrors.cqu.edu.cn/ubuntu-ports/",
	"http://mirrors.skyshe.cn/ubuntu-ports/",
	"mirrors.yun-idc.com/ubuntu-ports/",
	"http://mirror.dlut.edu.cn/ubuntu-ports/",
	"mirrors.xjtu.edu.cn/ubuntu-ports/",
	"mirrors.huaweicloud.com/repository/ubuntu-ports/",
	"mirrors.bupt.edu.cn/ubuntu-ports/",
	"mirrors.hit.edu.cn/ubuntu-ports/",
	"http://mirrors.sohu.com/ubuntu-ports/",
	"mirror.nju.edu.cn/ubuntu-ports/",
	"mirrors.bfsu.edu.cn/ubuntu-ports/",
	"mirror.lzu.edu.cn/ubuntu-ports/",
	"mirrors.aliyun.com/ubuntu-ports/",
	"ftp.sjtu.edu.cn/ubuntu-ports/",
	"mirrors.njupt.edu.cn/ubuntu-ports/",
	"mirrors.cloud.tencent.com/ubuntu-ports/",
	"http://mirrors.dgut.edu.cn/ubuntu-ports/",
	"mirrors.ustc.edu.cn/ubuntu-ports/",
	"mirrors.sdu.edu.cn/ubuntu-ports/",
	"http://cn.archive.ubuntu.com/ubuntu-ports/",
}

var UbuntuPortsCustomMirrors = []string{
	"mirrors.163.com/ubuntu-ports/",
}

var BuiltinUbuntuPortsMirrors = GenerateBuildInList(UbuntuPortsOfficialMirrors, UbuntuPortsCustomMirrors)

var UbuntuPortsDefaultCacheRules = newDebStyleRules(TypeUbuntuPorts)
