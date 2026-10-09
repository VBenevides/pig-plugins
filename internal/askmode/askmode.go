// Package askmode is the read-only policy behind /mode ask.
package askmode

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Prompt is appended to the system prompt while ask mode is on.
const Prompt = `

# ASK MODE (read-only) — enforced by the host

Ask mode is ON. You MUST NOT change anything: do not create, modify, delete, move or rename any file, do not run commands that write, install, commit or change state, and do not use tools with side effects.
Only read, search and explain. If the user asks for a change, say it is blocked by ask mode and tell them to run "/mode act".
The only exception: a file inside the project's .agent-work/ directory, and only when the user EXPLICITLY asked you in this conversation to write it. Such writes also need the user's confirmation.
Blocked tool calls are rejected by the host; do not try to work around them.`

// readOnlyTools never change state. todo keeps only in-memory/session notes.
var readOnlyTools = map[string]bool{
	"read": true, "grep": true, "find": true, "ls": true, "todo": true,
	"web_search": true, "url_context": true, "ask_user_question": true, "tool_search": true,
}

var readOnlyPrefixes = []string{"lsp_", "code_", "memory_"}

// ReadOnlyTool reports whether a non-bash, non-write tool is safe in ask mode.
func ReadOnlyTool(name string) bool {
	if readOnlyTools[name] {
		return true
	}
	for _, prefix := range readOnlyPrefixes {
		if strings.HasPrefix(name, prefix) && !strings.Contains(name, "rename_apply") {
			return true
		}
	}
	return false
}

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

var (
	segmentSplit = regexp.MustCompile(`&&|\|\||;|\||\n`)
	plainCommand = map[string]bool{
		"ls": true, "cat": true, "head": true, "tail": true, "grep": true, "rg": true, "wc": true, "file": true,
		"stat": true, "pwd": true, "echo": true, "which": true, "type": true, "date": true, "uname": true,
		"whoami": true, "tree": true, "du": true, "df": true, "diff": true, "sort": true, "uniq": true,
		"cut": true, "tr": true, "basename": true, "dirname": true, "realpath": true, "readlink": true,
		"jq": true, "nl": true, "column": true, "true": true, "false": true, "id": true, "hostname": true,
	}
	gitReadOnly = map[string]bool{
		"status": true, "diff": true, "log": true, "show": true, "blame": true, "ls-files": true,
		"rev-parse": true, "describe": true, "grep": true, "shortlog": true, "cat-file": true, "ls-tree": true,
	}
	findWriters = map[string]bool{"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true, "-fprint": true, "-fprint0": true, "-fprintf": true, "-fls": true}
)

// ReadOnlyBash reports whether command only reads. It is deliberately conservative: anything it cannot prove
// read-only is rejected, with a reason.
func ReadOnlyBash(command string) (bool, string) {
	if strings.TrimSpace(command) == "" {
		return false, "empty command"
	}
	for _, bad := range []string{">", "`", "$(", "<(", "<<"} {
		if strings.Contains(command, bad) {
			return false, fmt.Sprintf("contains %q (redirection or substitution)", bad)
		}
	}
	for _, segment := range segmentSplit.Split(command, -1) {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		if strings.Contains(segment, "&") {
			return false, "background execution is not allowed"
		}
		if ok, why := readOnlySegment(strings.Fields(segment)); !ok {
			return false, why
		}
	}
	return true, ""
}

func readOnlySegment(words []string) (bool, string) {
	name, args := words[0], words[1:]
	for _, a := range args {
		if a == "-o" || strings.HasPrefix(a, "--output") || a == "--pre" || strings.HasPrefix(a, "--pre=") {
			return false, fmt.Sprintf("%s %s may write or run a program", name, a)
		}
	}
	switch {
	case name == "find":
		for _, a := range args {
			if findWriters[a] {
				return false, "find " + a + " changes state"
			}
		}
		return true, ""
	case name == "git":
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				if a == "-c" || a == "-C" || strings.HasPrefix(a, "--exec-path") {
					return false, "git " + a + " is not allowed"
				}
				continue
			}
			if gitReadOnly[a] {
				return true, ""
			}
			return false, "git " + a + " is not read-only"
		}
		return false, "git without a read-only subcommand"
	case plainCommand[name]:
		return true, ""
	}
	return false, fmt.Sprintf("%q is not a known read-only command", name)
}
