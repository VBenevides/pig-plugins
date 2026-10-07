// Package agentdir resolves PiG's agent directory, where extensions keep their settings files.
package agentdir

import (
	"os"
	"path/filepath"
)

// Dir returns the agent directory: PIG_CODING_AGENT_DIR, else PIG_HOME/agent, else $HOME/.pig/agent.
// getenv is os.Getenv in production; tests inject a map lookup.
func Dir(getenv func(string) string) string {
	if dir := getenv("PIG_CODING_AGENT_DIR"); dir != "" {
		return dir
	}
	if home := getenv("PIG_HOME"); home != "" {
		return filepath.Join(home, "agent")
	}
	home := getenv("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	return filepath.Join(home, ".pig", "agent")
}

// File returns the path of a named file inside the agent directory.
func File(getenv func(string) string, name string) string {
	return filepath.Join(Dir(getenv), name)
}
