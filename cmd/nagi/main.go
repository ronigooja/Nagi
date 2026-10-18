package main

import (
	"os"

	"github.com/ronigooja/Nagi/internal/command"
	"github.com/ronigooja/Nagi/internal/requestrewriteservice"
)

var version = "dev"
var engineCommit = "unknown"

func main() {
	if len(os.Args) > 1 && os.Args[1] == requestrewriteservice.InternalArgument {
		if len(os.Args) != 2 {
			os.Exit(2)
		}
		os.Exit(requestrewriteservice.RunChild())
	}
	os.Exit(command.Run(os.Args[1:], os.Stdout, os.Stderr, version, engineCommit))
}
