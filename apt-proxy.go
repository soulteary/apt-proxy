package main

import (
	"github.com/apham0001/apt-proxy/cli"
)

func main() {
	flags, err := cli.ParseFlags()
	if err != nil {
		panic(err)
	}
	cli.Daemon(flags)
}
