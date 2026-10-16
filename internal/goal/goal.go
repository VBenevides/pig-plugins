// Package goal holds the pure logic of /goal: persisted state, skill expansion
// and the prompts that keep the model working until the objective is done.
package goal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EntryType is the session custom-entry type that stores goal state.
const EntryType = "pig-plugins-goal"

// MaxObjectiveBytes bounds an objective, which is also sent in every system prompt.
const MaxObjectiveBytes = 16 << 10

// MaxSummaryBytes bounds the model-written result summary.
const MaxSummaryBytes = 4 << 10

// Status is the lifecycle of a goal.
type Status string

const (
	Working  Status = "working"
	Finished Status = "finished"
)

// Outcome says why a finished goal stopped.
type Outcome string

const (
	Reached Outcome = "reached" // the objective is complete
	Blocked Outcome = "blocked" // everything left needs something the model cannot supply
	Stopped Outcome = "stopped" // interrupted or failed; not a statement about the objective
)

// State is one goal as stored in the session.
type State struct {
	Objective string  `json:"objective"`
	Status    Status  `json:"status"`
	Outcome   Outcome `json:"outcome,omitempty"`
	Summary   string  `json:"summary,omitempty"`
	// Cleared marks the goal as removed by /goal clear; such an entry restores to no goal.
	Cleared bool `json:"cleared,omitempty"`
}

// Restore returns the latest goal on the branch, or nil when there is none or it was cleared.
// Entries that cannot be decoded are reported, and the previous valid state is kept.
func Restore(branch []map[string]any) (*State, []string) {
	var current *State
	var warnings []string
	for i, entry := range branch {
		if entry["type"] != "custom" || entry["customType"] != EntryType {
			continue
		}
		raw, err := json.Marshal(entry["data"])
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("goal entry %d: %v", i, err))
			continue
		}
		var state State
		if err := json.Unmarshal(raw, &state); err != nil || (!state.Cleared && !state.valid()) {
			warnings = append(warnings, fmt.Sprintf("goal entry %d is malformed", i))
			continue
		}
		if state.Cleared {
			current = nil
			continue
		}
		current = &state
	}
	return current, warnings
}

func (s State) valid() bool {
	if strings.TrimSpace(s.Objective) == "" {
		return false
	}
	return s.Status == Working || s.Status == Finished
}

// Describe is the text of /goal check.
func Describe(state *State, idle bool) string {
	if state == nil {
		return "No goal. Use /goal set <objective>."
	}
	status := string(state.Status)
	switch {
	case state.Status == Working && idle:
		status = "working (idle: it continues after your next message)"
	case state.Status == Finished:
		status = "finished (" + string(state.Outcome) + ")"
	}
	text := "Current goal: " + state.Objective + "\nCurrent status: " + status
	if state.Summary != "" {
		text += "\nResult: " + state.Summary
	}
	return text
}

// SystemPrompt is appended to the system prompt on every run while the goal is working.
// It carries the rules so the per-turn nudge can stay short and survive compaction.
func SystemPrompt(objective string) string {
	return `

# ACTIVE GOAL

Objective: ` + objective + `

Work autonomously toward this objective and do not stop to ask for confirmation. Keep going until one of these is true:
- The objective is reached. Verify it with real checks (tests, builds, reading the result) before you claim it.
- Everything left is blocked by something you cannot resolve yourself (a decision only the user can make, missing access or credentials, an unavailable external system).
Then call goal_finish once with outcome "reached" or "blocked" and a short summary. Never call goal_finish while there is still work you can do. If one part is blocked, continue with the parts that are not.`
}

// ContinueMessage is sent when a run ended while the goal is still working.
const ContinueMessage = "The goal is still active and has not been finished. Continue working toward it. If it is reached, or everything left is blocked, call goal_finish."

// InitialMessage is the user message that starts a goal; skillBlocks come first, like a host skill expansion.
func InitialMessage(skillBlocks []string, objective string) string {
	text := "Goal: " + objective
	if len(skillBlocks) == 0 {
		return text
	}
	return strings.Join(skillBlocks, "\n\n") + "\n\n" + text
}

var skillToken = regexp.MustCompile(`(?:^|\s)/skill:([A-Za-z0-9_-]+)`)

// SkillNames lists the distinct /skill:name tokens in objective, in order of first mention.
func SkillNames(objective string) []string {
	var names []string
	seen := map[string]bool{}
	for _, match := range skillToken.FindAllStringSubmatch(objective, -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			names = append(names, match[1])
		}
	}
	return names
}

// SkillBlock reads the skill file at path and renders the block the host uses for /skill:name.
func SkillBlock(name, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read skill %q: %w", name, err)
	}
	return renderSkill(name, path, string(data)), nil
}

func renderSkill(name, path, content string) string {
	body := strings.TrimSpace(stripFrontmatter(content))
	return "<skill name=\"" + name + "\" location=\"" + path + "\">\n" +
		"References are relative to " + filepath.Dir(path) + ".\n\n" + body + "\n</skill>"
}

func stripFrontmatter(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return content
	}
	if _, body, found := strings.Cut(rest, "\n---"); found {
		_, body, _ = strings.Cut(body, "\n")
		return body
	}
	return content
}
