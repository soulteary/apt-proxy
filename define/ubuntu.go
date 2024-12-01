package define

import (
	"regexp"
)

const (
	UBUNTU_GEO_MIRROR_API = "http://mirrors.ubuntu.com/mirrors.txt"
	UBUNTU_BENCHMAKR_URL  = "dists/jammy/main/binary-amd64/Release"
)

var UBUNTU_HOST_PATTERN = regexp.MustCompile(`/ubuntu/(.+)$`)

// http://mirrors.ubuntu.com/mirrors.txt 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var UBUNTU_OFFICIAL_MIRRORS = []string{
	"mirrors.cn99.com/ubuntu/",
	"mirrors.tuna.tsinghua.edu.cn/ubuntu/",
	"mirrors.cnnic.cn/ubuntu/",
	"mirror.bjtu.edu.cn/ubuntu/",
	"mirrors.cqu.edu.cn/ubuntu/",
	"http://mirrors.skyshe.cn/ubuntu/",
	// duplicate "mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
	"mirrors.yun-idc.com/ubuntu/",
	"http://mirror.dlut.edu.cn/ubuntu/",
	"mirrors.xjtu.edu.cn/ubuntu/",
	"mirrors.huaweicloud.com/repository/ubuntu/",
	"mirrors.bupt.edu.cn/ubuntu/",
	"mirrors.hit.edu.cn/ubuntu/",
	// duplicate "repo.huaweicloud.com/ubuntu/",
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

var UBUNTU_CUSTOM_MIRRORS = []string{
	"mirrors.163.com/ubuntu/",
}

var BUILDIN_UBUNTU_MIRRORS = GenerateBuildInList(UBUNTU_OFFICIAL_MIRRORS, UBUNTU_CUSTOM_MIRRORS)

var UBUNTU_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`InRelease$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
	// Add file file hash
	{Pattern: regexp.MustCompile(`\/by-hash\/`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU},
}
