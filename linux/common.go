package linux

import "regexp"

const (
	UBUNTU string = "ubuntu"
	DEBIAN        = "debian"
)

type Rule struct {
	Pattern      *regexp.Regexp
	CacheControl string
	Rewrite      bool
}

const (
	mirrorTimeout    = 15 // seconds, detect resource timeout
	benchmarkTimes   = 3  // times, maximum number of attempts
	benchmarkTimeout = 10 // 10 seconds, for select fast mirror
)

// DEBIAN
const (
	DEBIAN_BENCHMAKR_URL = "dists/bullseye/main/binary-amd64/Release"
)

var DEBIAN_MIRROR_URLS = []string{
	"http://ftp.cn.debian.org/debian/",
	"http://mirror.bjtu.edu.cn/debian/",
	"http://mirror.lzu.edu.cn/debian/",
	"http://mirror.nju.edu.cn/debian/",
	"http://mirrors.163.com/debian/",
	"http://mirrors.bfsu.edu.cn/debian/",
	"http://mirrors.hit.edu.cn/debian/",
	"http://mirrors.huaweicloud.com/debian/",
	"http://mirrors.neusoft.edu.cn/debian/",
	"http://mirrors.tuna.tsinghua.edu.cn/debian/",
	"http://mirrors.ustc.edu.cn/debian/",
}

var DEBIAN_HOST_PATTERN = regexp.MustCompile(
	`https?://(deb|security|snapshot).debian.org/debian/(.+)$`,
)

var DEBIAN_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true},
	// Add file file hash
	{Pattern: regexp.MustCompile(`/by-hash/`), CacheControl: `max-age=3600`, Rewrite: true},
}

// Ubuntu
const (
	UBUNTU_MIRROR_URLS   = "http://mirrors.ubuntu.com/mirrors.txt"
	UBUNTU_BENCHMAKR_URL = "dists/jammy/main/binary-amd64/Release"
)

var UBUNTU_HOST_PATTERN = regexp.MustCompile(
	`https?://(security|archive).ubuntu.com/ubuntu/(.+)$`,
)

var UBUNTU_DEFAULT_CACHE_RULES = []Rule{
	{Pattern: regexp.MustCompile(`deb$`), CacheControl: `max-age=100000`, Rewrite: true},
	{Pattern: regexp.MustCompile(`udeb$`), CacheControl: `max-age=100000`, Rewrite: true},
	{Pattern: regexp.MustCompile(`DiffIndex$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`PackagesIndex$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Packages\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`SourcesIndex$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Sources\.(bz2|gz|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Release(\.gpg)?$`), CacheControl: `max-age=3600`, Rewrite: true},
	{Pattern: regexp.MustCompile(`Translation-(en|fr)\.(gz|bz2|bzip2|lzma)$`), CacheControl: `max-age=3600`, Rewrite: true},
	// Add file file hash
	{Pattern: regexp.MustCompile(`/by-hash/`), CacheControl: `max-age=3600`, Rewrite: true},
}
