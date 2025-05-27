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
	"http://mirrors.ircam.fr/pub/ubuntu-ports/",
	"http://fr.archive.ubuntu.com/ubuntu-ports/",
	"mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
}

var UBUNTU_PORTS_CUSTOM_MIRRORS = []string{}

var BUILDIN_UBUNTU_PORTS_MIRRORS = GenerateBuildInList(UBUNTU_PORTS_OFFICIAL_MIRRORS, UBUNTU_PORTS_CUSTOM_MIRRORS)

var UBUNTU_PORTS_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`InRelease$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
	// Add file file hash
	{Pattern: regexp.MustCompile(`\/by-hash\/`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_UBUNTU_PORTS},
}
