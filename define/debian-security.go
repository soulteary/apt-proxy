package define

import "regexp"

const (
	DEBIAN_SECURITY_BENCHMARK_URL = "dists/bookworm-security/main/binary-amd64/Release"
)

var DEBIAN_SECURITY_HOST_PATTERN = regexp.MustCompile(`/debian-security/(.+)$`)

var DEBIAN_SECURITY_OFFICIAL_MIRRORS = []string{
	"http://ftp.fr.debian.org/debian-security/",
	"http://deb.debian.org/debian-security/",
	"http://ftp.rezopole.net/debian-security/",
	"mirrors.tuna.tsinghua.edu.cn/debian-security/",
}

var DEBIAN_SECURITY_CUSTOM_MIRRORS = []string{}

var BUILDIN_DEBIAN_SECURITY_MIRRORS = GenerateBuildInList(DEBIAN_SECURITY_OFFICIAL_MIRRORS, DEBIAN_SECURITY_CUSTOM_MIRRORS)

var DEBIAN_SECURITY_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`InRelease$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
	{Pattern: regexp.MustCompile(`/by-hash/`), CacheControl: `max-age=3600`, Rewrite: true, OS: TYPE_LINUX_DISTROS_DEBIAN_SECURITY},
}
