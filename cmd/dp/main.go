package main

import (
	"os"

	"github.com/StatPan/datapan-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, cli.RealEnv{}, cli.RealHTTPClient{}))
}
