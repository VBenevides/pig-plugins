// Package contextprune is the PiG extension "context-prune": /context-prune on|off|status switches a model-free
// pruner. Requests reuse fixed decisions between eight-turn batches, unless context pressure requires earlier
// pruning. Full session history remains unchanged.
package contextprune

import (
	"fmt"
	"os"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/compactlimit"
	prune "github.com/VBenevides/pig-plugins/internal/contextprune"
)

// Name is the extension identity.
const Name = "context-prune"

const usage = "usage: /context-prune [status] | on | off | now | skills list|finish|keep|active|release [invocation-id]"

// RefreshEveryTurns bounds how long newly eligible output waits before a pruning attempt.
const RefreshEveryTurns = 8

// state holds the decisions of the current run.
type state struct {
	mu                           sync.Mutex
	enabled                      bool
	fresh                        bool // Decide again at the next request, because a run or a session change started.
	decisions                    prune.Decisions
	lastCount                    int
	turnsSinceRefresh            int
	sessionFile                  string
	forcePending                 bool // Manual request survives run start until the next model request.
	skillReasons                 map[string]string
	skillsRetained, skillsPruned int
}

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	st := &state{}
	file := func() string { return prune.File(os.Getenv) }

	e.RegisterCommand(Name, sdk.CommandOptions{
		Description: "Prune stale tool results from model requests (/context-prune on|off|status|now)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, value := range []string{"status", "on", "off", "now", "skills list", "skills finish", "skills keep", "skills active", "skills release"} {
				if strings.HasPrefix(value, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: value, Label: value})
				}
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
			if strings.TrimSpace(args) == "now" {
				message, level := st.now(ctx, file())
				ctx.Notify(message, level)
				return nil
			}
			if words := strings.Fields(args); len(words) > 0 && words[0] == "skills" {
				message, err := st.skillCommand(ctx, args)
				if err != nil {
					ctx.Notify(fmt.Sprintf("context-prune: %v", err), "error")
					return nil
				}
				ctx.Notify(message, "info")
				return nil
			}
			message, level := st.command(file(), args)
			ctx.Notify(message, level)
			return nil
		},
	})

	reset := func(ctx sdk.Context, _ map[string]any) (any, error) {
		st.startRun(ctx, file())
		return nil, nil
	}
	// A session, a branch or a compaction replaces the history, so earlier decisions no longer apply.
	e.OnEvent(sdk.EventAgentStart, reset)
	e.OnEvent(sdk.EventSessionStart, reset)
	e.OnEvent(sdk.EventSessionTree, reset)
	e.OnEvent(sdk.EventSessionCompact, reset)

	e.OnEvent(sdk.EventInput, func(ctx sdk.Context, data map[string]any) (any, error) {
		if !explicitInteraction(data) {
			return nil, nil
		}
		age, err := prune.SkillMaxAge(file())
		if err != nil {
			ctx.Notify(fmt.Sprintf("context-prune: interaction age unavailable: %v", err), "error")
			return nil, nil
		}
		if age == 0 {
			return nil, nil
		}
		if err := ctx.AppendEntry(prune.InteractionType, map[string]any{}); err != nil {
			return nil, fmt.Errorf("record skill interaction: %w", err)
		}
		return nil, nil
	})

	e.OnEvent(sdk.EventTurnEnd, func(_ sdk.Context, data map[string]any) (any, error) {
		message, _ := data["message"].(map[string]any)
		if message["stopReason"] == "toolUse" {
			st.mu.Lock()
			st.turnsSinceRefresh++
			st.mu.Unlock()
		}
		return nil, nil
	})

	e.OnEvent(sdk.EventContext, func(ctx sdk.Context, data map[string]any) (any, error) {
		messages, _ := data["messages"].([]any)
		pruned := st.apply(messages, func() bool { return overLimit(ctx) })
		if pruned == nil {
			return nil, nil
		}
		return map[string]any{"messages": pruned}, nil
	})
	return e
}

// startRun reads the switch and marks the decisions as stale. A damaged settings file is reported and counts as off.
func (st *state) startRun(ctx sdk.Context, file string) {
	enabled, err := prune.Enabled(file)
	if err != nil {
		ctx.Notify(fmt.Sprintf("%s: pruning is off: %v", Name, err), "error")
	}
	reasons := map[string]string{}
	maxAge, ageErr := prune.SkillMaxAge(file)
	if ageErr != nil {
		ctx.Notify(fmt.Sprintf("context-prune: skill pruning is off: %v", ageErr), "error")
	} else {
		entries, readErr := ctx.SessionManager().GetBranch(nil)
		if readErr != nil {
			ctx.Notify(fmt.Sprintf("context-prune: skill state unavailable: %v", readErr), "error")
		} else {
			reasons = prune.SkillReasons(prune.Lifetimes(entries), maxAge)
		}
	}
	sessionFile := ""
	if path, err := ctx.GetSessionFile(); err != nil {
		ctx.Notify(fmt.Sprintf("context-prune: saved-result recovery unavailable: %v", err), "error")
	} else if path != nil {
		sessionFile = *path
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.enabled, st.fresh = enabled, true
	st.skillReasons = reasons
	st.sessionFile, st.turnsSinceRefresh = sessionFile, 0
}

// overLimit reports whether the context passes the limit that /compact-settings set. A context that passes it
// inside a run gets a new round of decisions, so pruning can relieve the pressure and a summary is not needed.
func overLimit(ctx sdk.Context) bool {
	limits, err := compactlimit.Load(compactlimit.File(os.Getenv))
	if err != nil || !limits.IsSet() {
		return false
	}
	use, err := ctx.GetContextUsage()
	if err != nil || use == nil || use.Tokens == nil {
		return false
	}
	result := compactlimit.Effective(limits, use.ContextWindow, compactlimit.ReserveTokens(os.Getenv, ctx.Cwd()))
	return result.ShouldCompact(*use.Tokens)
}

// apply reuses the current prefix between batches. Each refresh measures only additional savings, so a
// large previous prune cannot justify repeated small cache-invalidating edits. Decisions only accumulate
// within a run; pressure can bring a batch forward without changing the compaction threshold.
func (st *state) apply(messages []any, overLimit func() bool) []any {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.applyLocked(messages, overLimit)
}

// applyLocked also supports a command preview without racing the next context event.
func (st *state) applyLocked(messages []any, overLimit func() bool) []any {
	if !st.enabled {
		return nil
	}
	settings := prune.DefaultSettings
	settings.SkillReasons = st.skillReasons
	settings.SessionFile = st.sessionFile
	force := st.forcePending
	st.forcePending = false
	if force {
		settings.MinSavingChars = 1
	}
	if st.fresh {
		st.decisions = prune.Decide(messages, settings)
		st.fresh = false
		st.turnsSinceRefresh = 0
	} else if force || st.turnsSinceRefresh >= RefreshEveryTurns || overLimit() {
		current, _ := prune.Apply(messages, st.decisions)
		more := prune.Decide(current, settings)
		if st.decisions == nil {
			st.decisions = prune.Decisions{}
		}
		for key, decision := range more {
			decision.Saved += st.decisions[key].Saved
			st.decisions[key] = decision
		}
		st.turnsSinceRefresh = 0
	}
	pruned, changed := prune.Apply(messages, st.decisions)
	st.lastCount = changed
	st.skillsPruned, st.skillsRetained = 0, 0
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		st.skillsRetained += len(prune.SkillBodies(m))
	}
	for _, raw := range pruned {
		m, _ := raw.(map[string]any)
		st.skillsPruned += len(prune.SkillBodies(m))
	}
	st.skillsPruned = st.skillsRetained - st.skillsPruned
	st.skillsRetained -= st.skillsPruned
	if changed == 0 {
		return nil
	}
	return pruned
}

// now selects eligible output immediately, without a summary call or a persistent setting change.
// A context view only takes effect when the next model request is built.
func (st *state) now(ctx sdk.Context, file string) (string, string) {
	entries, err := ctx.SessionManager().GetBranch(nil)
	if err != nil {
		return fmt.Sprintf("%s: could not prune now: %v", Name, err), "error"
	}
	st.startRun(ctx, file)
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.enabled {
		return Name + ": pruning is off. Use /context-prune on before /context-prune now.", "warning"
	}
	st.forcePending = true
	var messages []any
	for _, entry := range entries {
		switch entry["type"] {
		case "compaction":
			messages = nil
		case "message":
			if message := entry["message"]; message != nil {
				messages = append(messages, message)
			}
		}
	}
	st.applyLocked(messages, func() bool { return false })
	// Run start recomputes decisions, so retain the manual minimum-savings override for that request.
	st.forcePending = true
	return fmt.Sprintf("%s: now: prepared %d pruned messages, roughly %d tokens saved. Applies to the next model request; no LLM call. Recent results, errors and images remain protected.", Name, st.lastCount, st.decisions.Saved()/prune.CharsPerToken), "info"
}

// command runs one /context-prune command and returns the message to show.
func (st *state) command(file, args string) (string, string) {
	words := strings.Fields(args)
	if len(words) == 0 || words[0] == "status" {
		return st.status(file)
	}
	if len(words) != 1 || (words[0] != "on" && words[0] != "off") {
		return Name + ": " + usage, "warning"
	}
	enabled := words[0] == "on"
	if err := prune.SetEnabled(file, enabled); err != nil {
		return fmt.Sprintf("%s: could not save the setting: %v. Nothing was changed.", Name, err), "error"
	}
	st.mu.Lock()
	st.enabled, st.fresh = enabled, true
	st.forcePending = false
	st.mu.Unlock()
	if enabled {
		return Name + ": on. Pruning starts with the next agent run.", "info"
	}
	return Name + ": off.", "info"
}

func (st *state) status(file string) (string, string) {
	saved, err := prune.Enabled(file)
	if err != nil {
		return fmt.Sprintf("%s: %v; pruning is off.", Name, err), "error"
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	state := "off"
	if saved {
		state = "on"
	}
	chars := st.decisions.Saved()
	return fmt.Sprintf("%s: %s\ncurrent run: %d messages pruned in the last request, about %d characters (roughly %d tokens, an estimate) left out of each request\npolicy: refresh every %d tool-use turns or under pressure; protect %d tool-call groups and %d newest messages; require %d new characters (roughly %d tokens) per batch\nskill bodies: %d retained, %d pruned; only explicitly finished or released-and-expired bodies are eligible; recent messages and minimum savings still apply",
		Name, state, st.lastCount, chars, chars/prune.CharsPerToken, RefreshEveryTurns, prune.DefaultSettings.KeepRecentGroups, prune.DefaultSettings.KeepRecent, prune.DefaultSettings.MinSavingChars, prune.DefaultSettings.MinSavingChars/prune.CharsPerToken, st.skillsRetained, st.skillsPruned), "info"
}
