// Package pigplugins exposes the embedded distribution version.
package pigplugins

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version returns the version baked into this build.
func Version() string { return strings.TrimSpace(versionFile) }
