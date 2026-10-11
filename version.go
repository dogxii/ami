package buildinfo

import _ "embed"

// Version is embedded at build time, including builds installed with go install.
//
//go:embed VERSION
var Version string
