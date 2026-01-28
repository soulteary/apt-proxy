package main

import (
	"fmt"
	"os"

	"github.com/soulteary/apt-proxy/internal/cli"
)

func main() {
	flags, err := cli.ParseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cli.Daemon(flags)
}
