package define

import "regexp"

var ALPINE_HOST_PATTERN = regexp.MustCompile(`/alpine/(.+)$`)

const ALPINE_BENCHMAKR_URL = "MIRRORS.txt"

// https://mirrors.alpinelinux.org/ 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var ALPINE_OFFICIAL_MIRRORS = []string{
	"mirrors.tuna.tsinghua.edu.cn/alpine/",
	"mirrors.ustc.edu.cn/alpine/",
	"mirrors.nju.edu.cn/alpine/",
	// offline "mirror.lzu.edu.cn/alpine/",
	"mirrors.sjtug.sjtu.edu.cn/alpine/",
	"mirrors.aliyun.com/alpine/",
	// not vaild "mirrors.bfsu.edu.cn/alpine",
	// offline "mirrors.neusoft.edu.cn/alpine/",
}

var ALPINE_CUSTOM_MIRRORS = []string{}

var BUILDIN_ALPINE_MIRRORS = GenerateBuildInList(ALPINE_OFFICIAL_MIRRORS, ALPINE_CUSTOM_MIRRORS)

var ALPINE_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`APKINDEX.tar.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ALPINE},
	{Pattern: regexp.MustCompile(`tar.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ALPINE},
	{Pattern: regexp.MustCompile(`apk$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ALPINE},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ALPINE},
}
