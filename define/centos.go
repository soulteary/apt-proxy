package define

import "regexp"

var CENTOS_HOST_PATTERN = regexp.MustCompile(`/centos/(.+)$`)

const CENTOS_BENCHMAKR_URL = "TIME"

// https://www.centos.org/download/mirrors/ 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var CENTOS_OFFICIAL_MIRRORS = []string{
	"mirrors.bfsu.edu.cn/centos/",
	"mirrors.cqu.edu.cn/CentOS/",
	"http://mirrors.neusoft.edu.cn/centos/",
	"mirrors.nju.edu.cn/centos/",
	"mirrors.huaweicloud.com/centos/",
	"mirror.lzu.edu.cn/centos/",
	"http://mirrors.njupt.edu.cn/centos/",
	"mirrors.163.com/centos/",
	"mirrors.bupt.edu.cn/centos/",
	"ftp.sjtu.edu.cn/centos/",
	"mirrors.tuna.tsinghua.edu.cn/centos/",
	"mirrors.ustc.edu.cn/centos/",
}

var CENTOS_CUSTOM_MIRRORS = []string{
	"mirrors.aliyun.com/centos/",
}

var BUILDIN_CENTOS_MIRRORS = GenerateBuildInList(CENTOS_OFFICIAL_MIRRORS, CENTOS_CUSTOM_MIRRORS)

var CENTOS_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`repomd.xml$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`filelist.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`dir_sizes$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`TIME$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`timestamp.txt$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
}
