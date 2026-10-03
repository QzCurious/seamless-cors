package main

import (
	"os"

	"github.com/QzCurious/seamless-cors/internal/inbound/cli"
)

func main() {
	if err := cli.NewCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
