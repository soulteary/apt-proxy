package define

import "regexp"

var ROCKY_HOST_PATTERN = regexp.MustCompile(`/rocky/(.+)$`)

const ROCKY_BENCHMAKR_URL = "TIME"

// https://mirrors.rockylinux.org/ 2022.11.19
// Official Rocky Linux mirrors
var ROCKY_OFFICIAL_MIRRORS = []string{
	"https://dl.rockylinux.org/pub/rocky/",
	"https://rockylinux.mirrors.ovh.net/",
}

var ROCKY_CUSTOM_MIRRORS = []string{}

var BUILDIN_ROCKY_MIRRORS = GenerateBuildInList(ROCKY_OFFICIAL_MIRRORS, ROCKY_CUSTOM_MIRRORS)

var ROCKY_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`repomd.xml$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ROCKY},
	{Pattern: regexp.MustCompile(`filelist.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ROCKY},
	{Pattern: regexp.MustCompile(`dir_sizes$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ROCKY},
	{Pattern: regexp.MustCompile(`TIME$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ROCKY},
	{Pattern: regexp.MustCompile(`timestamp.txt$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ROCKY},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_ROCKY},
}
