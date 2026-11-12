// Package promptcheck tests the always-loaded prompt files: they must stay small, keep every essential rule, and
// state each rule once. It has no code; the files it checks are prompts/agent/*.md and the task-management guidance.
package promptcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// Default instructions plus task guidance stay below the former 11,240-byte baseline.
	maxTotalBytes   = 6_000
	maxSystemLines  = 50
	minSentenceSize = 60
)

var files = map[string]string{
	"SYSTEM.md":        filepath.Join("..", "..", "prompts", "agent", "SYSTEM.md"),
	"APPEND_SYSTEM.md": filepath.Join("..", "..", "prompts", "agent", "APPEND_SYSTEM.md"),
	"AGENTS.md":        filepath.Join("..", "..", "prompts", "agent", "AGENTS.md"),
	"guidance.md":      filepath.Join("..", "..", "extensions", "todo", "guidance.md"),
}

func load(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(data)
	}
	return out
}

func TestPromptsStaySmall(t *testing.T) {
	texts := load(t)
	total := 0
	for _, text := range texts {
		total += len(text)
	}
	if total > maxTotalBytes {
		t.Errorf("always-loaded prompts are %d bytes, budget %d", total, maxTotalBytes)
	}
	if lines := strings.Count(strings.TrimRight(texts["SYSTEM.md"], "\n"), "\n") + 1; lines > maxSystemLines {
		t.Errorf("SYSTEM.md is %d lines, budget %d", lines, maxSystemLines)
	}
}

// Every essential rule is present, and is present exactly once across all four files.
func TestEssentialRulesAppearExactlyOnce(t *testing.T) {
	texts := load(t)
	all := strings.Join([]string{texts["SYSTEM.md"], texts["APPEND_SYSTEM.md"], texts["AGENTS.md"], texts["guidance.md"]}, "\n")
	rules := map[string]string{
		"hashline anchor format":        "12#a3f9",
		"stale anchor recovery":         "reread and retry",
		"no validation bypass":          "Never bypass validation",
		"single edit call per file":     "single edit call",
		"no overwrite with write":       "Use write only for new files",
		"no hand edits through bash":    "Never use bash, scripts, redirection, patches, or write for those changes",
		"multi-file, formatter and LSP": "Multi-file changes are allowed",
		"no mutation-tool bypass":       "Never use these tools to bypass a blocked edit",
		"safety blocks respected":       "Respect safety blocks and approval prompts",
		"no unverified claims":          "never claim unperformed verification",
		"smallest change":               "Make the smallest complete change",
		"todo only for substantial":     "Use `todo` only for substantial multi-step work",
		"untrusted content":             "untrusted data",
		"preserve the working tree":     "Do not reset, discard, stage, commit",
		"no weakened checks":            "weaken checks",
		"authorization for destruction": "Get approval for destructive operations",
		"handled errors observable":     "Keep errors observable",
		"no hardcoded secrets":          "Never hardcode credentials or secrets",
		"tool discovery":                "tool_search",
	}
	for name, phrase := range rules {
		if got := strings.Count(all, phrase); got != 1 {
			t.Errorf("rule %q (%q) appears %d times, want exactly 1", name, phrase, got)
		}
	}
	if strings.Contains(texts["SYSTEM.md"], "<todo_usage>") {
		t.Error("SYSTEM.md must not repeat the todo guidance the todo extension injects")
	}
	for _, needed := range []string{"<Task_Management>", "never IDs", "append"} {
		if !strings.Contains(texts["guidance.md"], needed) {
			t.Errorf("todo guidance lost %q, which the todo extension tests rely on", needed)
		}
	}
}

// No long sentence may be repeated in two files: a rule has one owner.
func TestNoSentenceIsRepeatedAcrossFiles(t *testing.T) {
	split := regexp.MustCompile(`[.\n]\s+`)
	space := regexp.MustCompile(`\s+`)
	owner := map[string]string{}
	for name, text := range load(t) {
		for _, sentence := range split.Split(text, -1) {
			key := strings.ToLower(space.ReplaceAllString(strings.Trim(sentence, " -*\n"), " "))
			if len(key) < minSentenceSize {
				continue
			}
			if previous, seen := owner[key]; seen && previous != name {
				t.Errorf("sentence repeated in %s and %s: %q", previous, name, key)
			}
			owner[key] = name
		}
	}
}
