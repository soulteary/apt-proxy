package define

import "regexp"

var CENTOS_HOST_PATTERN = regexp.MustCompile(`/centos/(.+)$`)

const CENTOS_BENCHMAKR_URL = "TIME"

// https://www.centos.org/download/mirrors/ 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var CENTOS_OFFICIAL_MIRRORS = []string{
	"http://mirror.centos.org/centos/",
	"http://centos.mirrors.proxad.net/centos/",
	"http://centos.mirrors.ovh.net/ftp.centos.org/",
	"http://ftp.rezopole.net/centos/",
	"http://mirror.ibcp.fr/pub/centos/",
	"mirrors.tuna.tsinghua.edu.cn/centos/",
}

var CENTOS_CUSTOM_MIRRORS = []string{}

var BUILDIN_CENTOS_MIRRORS = GenerateBuildInList(CENTOS_OFFICIAL_MIRRORS, CENTOS_CUSTOM_MIRRORS)

var CENTOS_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`repomd.xml$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`filelist.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`dir_sizes$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`TIME$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`timestamp.txt$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_CENTOS},
}
