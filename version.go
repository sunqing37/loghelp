package loghelp

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is embedded at build time from the repository's VERSION file.
var Version = strings.TrimSpace(versionFile)

//go:embed REVISION
var revisionFile string

// Revision distinguishes portable packages with the same application version.
var Revision = readRevision()

func readRevision() uint64 {
	revision, err := strconv.ParseUint(strings.TrimSpace(revisionFile), 10, 64)
	if err != nil {
		panic("invalid embedded REVISION: " + err.Error())
	}
	return revision
}
