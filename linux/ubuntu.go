package linux

import "regexp"

const (
	mirrorsUrl       = "http://mirrors.ubuntu.com/mirrors.txt"
	mirrorTimeout    = 15 //seconds
	benchmarkUrl     = "dists/jammy/main/binary-amd64/Release"
	benchmarkTimes   = 3
	benchmarkBytes   = 1024 * 512 // 512Kb
	benchmarkTimeout = 10         // 10 seconds
)

var hostPattern = regexp.MustCompile(
	`https?://(\w{2}.)?(security|archive).ubuntu.com/ubuntu/(.+)$`,
)
