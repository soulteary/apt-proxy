package linux

import "regexp"

const (
	mirrorsUrl   = "http://mirrors.ubuntu.com/mirrors.txt"
	benchmarkUrl = "dists/jammy/main/binary-amd64/Release"
)

var hostPattern = regexp.MustCompile(
	`https?://(\w{2}.)?(security|archive).ubuntu.com/ubuntu/(.+)$`,
)
