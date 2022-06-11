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

var hostPattern = regexp.MustCompile(
	`https?://(\w{2}.)?(security|archive).ubuntu.com/ubuntu/(.+)$`,
)
