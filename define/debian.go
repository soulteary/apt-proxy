package define

import "regexp"

const (
	DEBIAN_BENCHMAKR_URL = "dists/bullseye/main/binary-amd64/Release"
)

var DEBIAN_HOST_PATTERN = regexp.MustCompile(`/debian/(.+)$`)

// https://www.debian.org/mirror/list 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var DEBIAN_OFFICIAL_MIRRORS = []string{
	"http://ftp.cn.debian.org/debian/",
	"mirror.bjtu.edu.cn/debian/",
	"mirrors.163.com/debian/",
	"mirrors.bfsu.edu.cn/debian/",
	"mirrors.huaweicloud.com/debian/",
	"http://mirrors.neusoft.edu.cn/debian/",
	"mirrors.tuna.tsinghua.edu.cn/debian/",
	"mirrors.ustc.edu.cn/debian/",
}

var DEBIAN_CUSTOM_MIRRORS = []string{
	"repo.huaweicloud.com/debian/",
	"mirrors.cloud.tencent.com/debian/",
	"mirrors.hit.edu.cn/debian/",
	"mirrors.aliyun.com/debian/",
	"mirror.lzu.edu.cn/debian/",
	"mirror.nju.edu.cn/debian/",
}

var BUILDIN_DEBIAN_MIRRORS = GenerateBuildInList(DEBIAN_OFFICIAL_MIRRORS, DEBIAN_CUSTOM_MIRRORS)

var DEBIAN_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`InRelease$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
	// Add file file hash
	{Pattern: regexp.MustCompile(`/by-hash/`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN},
}
