package main

import (
	"github.com/soulteary/apt-proxy/cli"
)

func main() {
	flags, err := cli.ParseFlags()
	if err != nil {
		panic(err)
	}
	cli.Daemon(flags)
}
