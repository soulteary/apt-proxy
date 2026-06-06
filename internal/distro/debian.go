package distro

import "regexp"

const (
	DebianBenchmarkURL = "dists/bullseye/main/binary-amd64/Release"
)

var DebianHostPattern = regexp.MustCompile(`/debian(-security)?/(.+)$`)

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
