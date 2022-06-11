package linux

import "regexp"

const (
	ALPINE_MIRROR_URLS   = "http://dl-cdn.alpinelinux.org/alpine/MIRRORS.txt"
	ALPINE_BENCHMAKR_URL = "/alpine/MIRRORS.txt"
)

var ALPINE_HOST_PATTERN = regexp.MustCompile(
	`https?://dl-cdn.alpinelinux.org/alpine/(.+)$`,
)
