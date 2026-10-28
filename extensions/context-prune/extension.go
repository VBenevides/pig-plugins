// Package contextprune is the PiG extension "context-prune": /context-prune on|off|status switches a model-free
// pruner. At the start of each agent run it decides which old tool results and skill bodies the provider requests of
// that run leave out. The decisions stay fixed for the whole run, so the request prefix does not change between the
// requests of one run and the provider prefix cache keeps working. The session history is not changed.
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

const usage = "usage: /context-prune [status] | on | off | skills list|finish|keep|active|release [invocation-id]"

// state holds the decisions of the current run.
type state struct {
	mu        sync.Mutex
	enabled   bool
	fresh     bool // Decide again at the next request, because a run or a session change started.
	decisions prune.Decisions
	lastCount int
}

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	st := &state{}
	file := func() string { return prune.File(os.Getenv) }

	e.RegisterCommand(Name, sdk.CommandOptions{
		Description: "Prune stale tool results from model requests (/context-prune on|off|status)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, value := range []string{"status", "on", "off", "skills list", "skills finish", "skills keep", "skills active", "skills release"} {
				if strings.HasPrefix(value, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: value, Label: value})
				}
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
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
	st.mu.Lock()
	defer st.mu.Unlock()
	st.enabled, st.fresh = enabled, true
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

// apply returns the pruned message list, or nil when nothing changes. The decisions stay fixed inside a run, except
// that more are added when the context is over the limit. That change costs one prefix cache miss, as a summary
// would, and it only happens when the pruner can remove more than it already does.
func (st *state) apply(messages []any, overLimit func() bool) []any {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.enabled {
		return nil
	}
	if st.fresh {
		st.decisions = prune.Decide(messages, prune.DefaultSettings)
		st.fresh = false
	} else if overLimit() {
		if more := prune.Decide(messages, prune.DefaultSettings); more.Saved() > st.decisions.Saved() {
			st.decisions = more
		}
	}
	pruned, changed := prune.Apply(messages, st.decisions)
	st.lastCount = changed
	if changed == 0 {
		return nil
	}
	return pruned
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
	return fmt.Sprintf("%s: %s\ncurrent run: %d messages pruned in the last request, about %d characters (roughly %d tokens, an estimate) left out of each request",
		Name, state, st.lastCount, chars, chars/prune.CharsPerToken), "info"
}
