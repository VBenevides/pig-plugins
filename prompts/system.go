// Package prompts embeds the bundled project guidance.
package prompts

import _ "embed"

// ProjectSystem is appended to the host prompt, not used as its replacement.
//
//go:embed project-system.md
var ProjectSystem string
