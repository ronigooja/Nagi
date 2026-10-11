// Package scripts embeds the local installer used by installed CLI commands.
package scripts

import _ "embed"

// Installer is shared with the source and release installation workflows.
//
//go:embed installer.sh
var Installer string
