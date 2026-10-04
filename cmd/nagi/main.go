package main

import (
	"os"

	"github.com/ronigooja/Nagi/internal/command"
)

var version = "dev"
var engineCommit = "unknown"

func main() {
	os.Exit(command.Run(os.Args[1:], os.Stdout, os.Stderr, version, engineCommit))
}
