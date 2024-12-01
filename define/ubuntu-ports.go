package define

import (
	"regexp"
)

const (
	UBUNTU_PORTS_GEO_MIRROR_API = "http://mirrors.ubuntu.com/mirrors.txt"
	UBUNTU_PORTS_BENCHMAKR_URL  = "dists/noble/InRelease/Release"
)

var UBUNTU_PORTS_HOST_PATTERN = regexp.MustCompile(`/ubuntu-ports/(.+)$`)

// http://mirrors.ubuntu.com/mirrors.txt 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var UBUNTU_PORTS_OFFICIAL_MIRRORS = []string{
	"mirrors.cn99.com/ubuntu-ports/",
	"mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
	"mirrors.cnnic.cn/ubuntu-ports/",
	"mirror.bjtu.edu.cn/ubuntu-ports/",
	"mirrors.cqu.edu.cn/ubuntu-ports/",
	"http://mirrors.skyshe.cn/ubuntu-ports/",
	// duplicate "mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
	"mirrors.yun-idc.com/ubuntu-ports/",
	"http://mirror.dlut.edu.cn/ubuntu-ports/",
	"mirrors.xjtu.edu.cn/ubuntu-ports/",
	"mirrors.huaweicloud.com/repository/ubuntu-ports/",
	"mirrors.bupt.edu.cn/ubuntu-ports/",
	"mirrors.hit.edu.cn/ubuntu-ports/",
	// duplicate "repo.huaweicloud.com/ubuntu-ports/",
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

var UBUNTU_PORTS_CUSTOM_MIRRORS = []string{
	"mirrors.163.com/ubuntu-ports/",
}

var BUILDIN_UBUNTU_PORTS_MIRRORS = GenerateBuildInList(UBUNTU_PORTS_OFFICIAL_MIRRORS, UBUNTU_PORTS_CUSTOM_MIRRORS)

var UBUNTU_PORTS_DEFAULT_CACHE_RULES = []Rule{
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
