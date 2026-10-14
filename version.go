package loghelp

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is embedded at build time from the repository's VERSION file.
var Version = strings.TrimSpace(versionFile)
