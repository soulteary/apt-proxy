package define

import "regexp"

var FEDORA_HOST_PATTERN = regexp.MustCompile(`/fedora/(.+)$`)

const FEDORA_BENCHMAKR_URL = "TIME"

// https://mirrors.fedoraproject.org/ 2022.11.19
// Official Fedora mirrors
var FEDORA_OFFICIAL_MIRRORS = []string{
	"https://download.fedoraproject.org/pub/fedora/linux/",
	"https://mirror.ufs.ac.za/fedora/",
	"http://ftp.nluug.nl/pub/os/Linux/distr/fedora/",
}

var FEDORA_CUSTOM_MIRRORS = []string{}

var BUILDIN_FEDORA_MIRRORS = GenerateBuildInList(FEDORA_OFFICIAL_MIRRORS, FEDORA_CUSTOM_MIRRORS)

var FEDORA_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`repodata/.*\.(xml|xml.gz|xml.xz|json)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_FEDORA},
	{Pattern: regexp.MustCompile(`repodata/.*\.(zck|zst)$`), CacheControl: ``, Rewrite: false, OS: TYPE_LINUX_DISTROS_FEDORA},
	{Pattern: regexp.MustCompile(`Packages/.*\.rpm$`), CacheControl: `max-age=86400`, Rewrite: true, OS: TYPE_LINUX_DISTROS_FEDORA},
	{Pattern: regexp.MustCompile(`.*timestamp.txt$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_FEDORA},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_FEDORA},
}
