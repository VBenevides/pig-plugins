package betterfooter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

const recentStateVersion = 1

// ThinkingLevels are the levels a saved state may name.
var ThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// IsThinkingLevel reports whether value names a thinking level.
func IsThinkingLevel(value string) bool { return slices.Contains(ThinkingLevels, value) }

// ModelRef identifies a model by provider and id.
type ModelRef struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// RecentState is the most recent model and thinking level of a working directory.
type RecentState struct {
	Version       int    `json:"version"`
	Provider      string `json:"provider"`
	ModelID       string `json:"modelId"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// Ref is the state's model.
func (s RecentState) Ref() ModelRef { return ModelRef{s.Provider, s.ModelID} }

// RecentStatePath is <agent dir>/recent-models/<sha256 of the absolute cwd>.json.
func RecentStatePath(getenv func(string) string, cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = filepath.Clean(cwd)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(agentdir.Dir(getenv), "recent-models", hex.EncodeToString(sum[:])+".json")
}

// LoadRecentState reads the saved state. found is false for a missing file and for a state this version does not
// understand (wrong version or missing fields); an unreadable or corrupt file is an error, so it is reported
// instead of silently losing the saved model. An invalid thinking level is dropped, not the whole state.
func LoadRecentState(path string) (state RecentState, found bool, err error) {
	var raw struct {
		Version       *int     `json:"version"`
		Provider      *string  `json:"provider"`
		ModelID       *string  `json:"modelId"`
		ThinkingLevel any      `json:"thinkingLevel"`
		UpdatedAt     *float64 `json:"updatedAt"`
	}
	ok, err := readJSONCapped(path, &raw)
	if err != nil || !ok {
		return RecentState{}, false, err
	}
	if raw.Version == nil || *raw.Version != recentStateVersion || raw.Provider == nil || raw.ModelID == nil ||
		raw.UpdatedAt == nil || math.IsNaN(*raw.UpdatedAt) || math.IsInf(*raw.UpdatedAt, 0) {
		return RecentState{}, false, nil
	}
	state = RecentState{Version: recentStateVersion, Provider: *raw.Provider, ModelID: *raw.ModelID, UpdatedAt: int64(*raw.UpdatedAt)}
	if level, isString := raw.ThinkingLevel.(string); isString && IsThinkingLevel(level) {
		state.ThinkingLevel = level
	}
	return state, true, nil
}

// SaveRecentState records ref and level at updatedAt (Unix milliseconds) atomically with 0600 permissions. A
// state saved by another process with a newer updatedAt is kept; written reports whether the file changed.
func SaveRecentState(path string, ref ModelRef, level string, updatedAt int64) (written bool, err error) {
	existing, found, err := LoadRecentState(path)
	if err != nil {
		return false, fmt.Errorf("read existing recent model: %w", err)
	}
	if found && existing.UpdatedAt > updatedAt {
		return false, nil
	}
	state := RecentState{Version: recentStateVersion, Provider: ref.Provider, ModelID: ref.ModelID, UpdatedAt: updatedAt}
	if IsThinkingLevel(level) {
		state.ThinkingLevel = level
	}
	data, err := json.Marshal(state)
	if err != nil {
		return false, fmt.Errorf("encode recent model: %w", err)
	}
	if err := fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// StartupOption returns the value of a CLI option the way Pi's own parser reads it: `--name value` or
// `--name=value`, the last occurrence wins, and parsing stops at `--`. ok is false when the option is absent.
func StartupOption(args []string, name string) (value string, ok bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return value, ok
		case arg == name:
			ok = true
			value = ""
			if i+1 < len(args) {
				i++
				value = args[i]
			}
		case strings.HasPrefix(arg, name+"="):
			value, ok = arg[len(name)+1:], true
		}
	}
	return value, ok
}

// ShouldRestore decides whether a session start restores the saved model. reason is the session_start reason;
// args are Pi's CLI arguments and hasConversation tells whether the session already has messages (a resumed one,
// which Pi restores itself). An explicit --model or --provider always wins on startup.
func ShouldRestore(reason string, args []string, hasConversation, disabled bool) bool {
	if disabled {
		return false
	}
	switch reason {
	case "new":
		return true
	case "startup":
		if _, ok := StartupOption(args, "--model"); ok {
			return false
		}
		if _, ok := StartupOption(args, "--provider"); ok {
			return false
		}
		return !hasConversation
	}
	return false
}

// RestoreThinkingLevel picks the level to apply after restoring: an explicit --thinking, then the level of the
// scoped model for an explicit --models, then the saved level. "" means leave the level alone.
func RestoreThinkingLevel(reason string, args []string, explicitScope bool, scopedLevel, saved string) string {
	if reason == "startup" {
		if override, ok := StartupOption(args, "--thinking"); ok && IsThinkingLevel(override) {
			return override
		}
	}
	if explicitScope && scopedLevel != "" {
		return scopedLevel
	}
	return saved
}
