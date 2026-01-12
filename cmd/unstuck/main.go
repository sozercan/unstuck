package main

import (
	"os"

	"github.com/sozercan/unstuck/pkg/cli"
)

// Version information set via ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd := cli.NewRootCommand(version, commit, date)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
