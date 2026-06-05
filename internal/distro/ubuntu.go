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

var UbuntuDefaultCacheRules = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`InRelease$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
	{Pattern: regexp.MustCompile(`\/by-hash\/`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeUbuntu},
}
