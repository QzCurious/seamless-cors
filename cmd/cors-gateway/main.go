package main

import (
	"os"

	"cors-vpn/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
