// Package askmode is the read-only policy behind /mode ask.
package askmode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Prompt is appended to the system prompt while ask mode is on.
const Prompt = `

# ASK MODE (read-only) — enforced by the host

Ask mode is ON. You MUST NOT change anything: do not create, modify, delete, move or rename any file, do not run commands that write, install, commit or change state, and do not use tools with side effects.
Only read, search and explain. If the user asks for a change, say it is blocked by ask mode and tell them to run "/mode act".
The only exception: a file inside the project's .agent-work/ directory, and only when the user EXPLICITLY asked you in this conversation to write it. Such writes also need the user's confirmation.
Blocked tool calls are rejected by the host; do not try to work around them.`

// readOnlyTools never change project files. Task management keeps only in-memory/session notes.
var readOnlyTools = map[string]bool{
	"read": true, "grep": true, "find": true, "ls": true, "todo": true,
	"web_search": true, "url_context": true, "ask_user_question": true, "tool_search": true,
	// These are the reviewed tools shipped by lsp and pi-curator. New names
	// require policy review, even when discovered under the same namespace.
	"lsp_diagnostics": true, "lsp_hover": true, "lsp_definition": true,
	"lsp_references": true, "lsp_symbols": true, "lsp_rename_preview": true,
	"code_overview": true, "code_search": true,
	"memory_search": true, "memory_read": true,
}

// ReadOnlyTool admits only exact, reviewed names. Discovery never grants rights.
func ReadOnlyTool(name string) bool { return readOnlyTools[name] }

// WriteTool reports whether the tool writes to a path it receives.
func WriteTool(name string) bool { return name == "write" || name == "edit" }

// InAgentWork reports whether path (resolved against cwd, following symlinks) is inside cwd/.agent-work.
func InAgentWork(cwd, path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("empty path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	resolved, err := resolve(filepath.Clean(path))
	if err != nil {
		return false, fmt.Errorf("resolve %q: %w", path, err)
	}
	root, err := resolve(filepath.Join(cwd, ".agent-work"))
	if err != nil {
		return false, fmt.Errorf("resolve .agent-work: %w", err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return false, nil
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// resolve evaluates symlinks of the longest existing ancestor and re-appends the missing tail.
func resolve(path string) (string, error) {
	tail := ""
	for cur := path; ; {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(real, tail), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

// ReadOnlyBash rejects shell execution in ASK. Command names and argument
// filtering cannot establish read-only behavior: PATH, configuration, filters,
// and subprocesses can all introduce side effects. Use dedicated read tools.
// This policy is not an OS sandbox for trusted extension code.
func ReadOnlyBash(_ string) (bool, string) {
	return false, "shell execution cannot be verified read-only; use dedicated read/search tools"
}
