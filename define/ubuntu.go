package define

import (
	"regexp"
)

const (
	UBUNTU_GEO_MIRROR_API = "http://mirrors.ubuntu.com/mirrors.txt"
	UBUNTU_BENCHMAKR_URL  = "dists/noble/main/binary-amd64/Release"
)

var UBUNTU_HOST_PATTERN = regexp.MustCompile(`/ubuntu/(.+)$`)

// http://mirrors.ubuntu.com/mirrors.txt 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var UBUNTU_OFFICIAL_MIRRORS = []string{
	"http://fr.archive.ubuntu.com/ubuntu/",
	"http://mirrors.ircam.fr/pub/ubuntu/",
	"mirrors.tuna.tsinghua.edu.cn/ubuntu/",
}

var UBUNTU_CUSTOM_MIRRORS = []string{}

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
