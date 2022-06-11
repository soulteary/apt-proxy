package linux

import "regexp"

const (
	mirrorTimeout    = 15 // seconds, detect resource timeout
	benchmarkTimes   = 3  // times, maximum number of attempts
	benchmarkTimeout = 10 // 10 seconds, for select fast mirror
)

// Alpine
const (
	ALPINE_MIRROR_URLS   = "http://dl-cdn.alpinelinux.org/alpine/MIRRORS.txt"
	ALPINE_BENCHMAKR_URL = "/alpine/MIRRORS.txt"
)

var ALPINE_HOST_PATTERN = regexp.MustCompile(
	`https?://dl-cdn.alpinelinux.org/alpine/(.+)$`,
)

// Ubuntu
const (
	UBUNTU_MIRROR_URLS   = "http://mirrors.ubuntu.com/mirrors.txt"
	UBUNTU_BENCHMAKR_URL = "dists/jammy/main/binary-amd64/Release"
)

var UBUNTU_HOST_PATTERN = regexp.MustCompile(
	`https?://(security|archive).ubuntu.com/ubuntu/(.+)$`,
)

type Rule struct {
	Pattern      *regexp.Regexp
	CacheControl string
	Rewrite      bool
}

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
