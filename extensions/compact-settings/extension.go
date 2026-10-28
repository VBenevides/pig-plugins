// Package compactsettings is the PiG extension "compact-settings": /compact-settings limit relative|absolute sets
// a context-size limit for automatic compacting. The effective limit is min(relative, absolute, window - reserve),
// so PiG's own threshold always remains the upper bound. The extension checks before idle input starts a new
// run and after a run settles, using the same path as /compact. The built-in /compact command is not touched.
// Active-run input never waits for compaction: manual compaction aborts and waits for that run.
package compactsettings

import (
	"fmt"
	"os"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/betterfooter"
	"github.com/VBenevides/pig-plugins/internal/compactlimit"
)

// Name is the extension identity.
const Name = "compact-settings"

const usage = "usage: /compact-settings [status] | limit [status] | limit relative <0-100|off> | limit absolute <tokens like 200000, 200k, 1M|off> | limit off"
const commandPrefix = Name + ": "
const unchangedSuffix = ". Nothing was changed."

// state serializes settings changes and the compact trigger.
type state struct {
	mu         sync.Mutex
	compacting bool
	done       chan struct{} // Closed by the compact callback, never while a waiter holds mu.
	failedAt   int           // Context size at which compacting last failed; the same size is not retried.
	// afterCompact is set when a mid-run compact finishes, and floor holds the first context size measured after
	// it. A mid-run compact repeats only after the context grew past that floor: the summary and kept messages can
	// stay above the limit, and compacting again cannot shrink them. The settled hook clears both.
	afterCompact bool
	floor        int
}

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	st := &state{failedAt: -1}

	e.RegisterCommand("compact-settings", sdk.CommandOptions{
		Description: "Show or set the automatic compact limit (/compact-settings limit relative <0-100> | absolute <tokens>)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, value := range []string{"status", "limit status", "limit relative", "limit absolute", "limit off"} {
				if strings.HasPrefix(value, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: value, Label: value})
				}
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
			st.mu.Lock()
			defer st.mu.Unlock()
			message, level := handle(ctx, args)
			ctx.Notify(message, level)
			return nil
		},
	})

	e.OnEvent(sdk.EventInput, func(ctx sdk.Context, _ map[string]any) (any, error) {
		idle, err := ctx.IsIdle()
		if err != nil {
			return nil, fmt.Errorf("%s: check idle before input: %w", Name, err)
		}
		if !idle {
			return nil, nil
		}
		if err := st.trigger(ctx, false); err != nil {
			return nil, err
		}
		st.mu.Lock()
		done := st.done
		st.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return nil, fmt.Errorf("%s: input cancelled while waiting for compaction", Name)
			}
		}
		return nil, nil
	})

	// Runs after the run has fully settled, like a /compact typed by the user: the host rejects a prompt submitted
	// while the compact is in progress ("Cannot submit a prompt while compaction is in progress"). Waiting inside
	// agent_before_settle was tried and deadlocks, because the compact aborts the run that is waiting for it.
	e.OnEvent(sdk.EventAgentSettled, func(ctx sdk.Context, _ map[string]any) (any, error) {
		st.endRun()
		return nil, st.trigger(ctx, false)
	})

	// A tool-use turn means the run goes on to another model request. Without this check a long autonomous run
	// grows far past the limit, because the two hooks above only fire between runs. The compact aborts the run, so
	// it resumes with a follow-up message once the compact finishes.
	e.OnEvent(sdk.EventTurnEnd, func(ctx sdk.Context, data map[string]any) (any, error) {
		if !continuesRun(data) {
			return nil, nil
		}
		return nil, st.trigger(ctx, true)
	})
	return e
}

// handle runs one /compact-settings command and returns the message to show.
func handle(ctx sdk.Context, args string) (string, string) {
	file := compactlimit.File(os.Getenv)
	words := strings.Fields(args)
	if len(words) == 0 || words[0] == "status" || (len(words) == 1 && words[0] == "limit") || (len(words) == 2 && words[0] == "limit" && words[1] == "status") {
		return describe(ctx, file)
	}
	if words[0] != "limit" {
		return fmt.Sprintf("compact-settings: unknown option %q; %s", words[0], usage), "warning"
	}
	limits, err := compactlimit.Load(file)
	if err != nil {
		return fmt.Sprintf("compact-settings: %v. Fix or delete the file before changing the limit; nothing was changed.", err), "error"
	}
	switch {
	case len(words) == 2 && isOff(words[1]):
		limits = compactlimit.Limits{}
	case len(words) == 3 && words[1] == "relative":
		if isOff(words[2]) {
			limits.Relative = nil
			break
		}
		percent, err := compactlimit.ParsePercent(words[2])
		if err != nil {
			return commandPrefix + err.Error() + unchangedSuffix, "warning"
		}
		limits.Relative = &percent
	case len(words) == 3 && words[1] == "absolute":
		if isOff(words[2]) {
			limits.Absolute = nil
			break
		}
		tokens, err := compactlimit.ParseTokens(words[2])
		if err != nil {
			return commandPrefix + err.Error() + unchangedSuffix, "warning"
		}
		limits.Absolute = &tokens
	default:
		return commandPrefix + usage + unchangedSuffix, "warning"
	}
	if err := compactlimit.Save(file, limits); err != nil {
		return fmt.Sprintf("compact-settings: could not save the limit: %v. Nothing was changed.", err), "error"
	}
	message, level := describe(ctx, file)
	return "compact-settings: saved.\n" + message, level
}

func isOff(word string) bool { return word == "off" || word == "clear" }

// describe reports the saved limits and the effective limit for the current model.
func describe(ctx sdk.Context, file string) (string, string) {
	limits, err := compactlimit.Load(file)
	if err != nil {
		return fmt.Sprintf("compact-settings: %v; PiG's own threshold applies.", err), "error"
	}
	var lines []string
	if limits.Relative != nil {
		lines = append(lines, fmt.Sprintf("limit relative: %s%% of the context window", trimPercent(*limits.Relative)))
	} else {
		lines = append(lines, "limit relative: off")
	}
	if limits.Absolute != nil {
		lines = append(lines, fmt.Sprintf("limit absolute: %s tokens (%d)", betterfooter.FormatTokens(*limits.Absolute), *limits.Absolute))
	} else {
		lines = append(lines, "limit absolute: off")
	}
	use, err := ctx.GetContextUsage()
	if err != nil || use == nil || use.ContextWindow <= 0 {
		return strings.Join(append(lines, "effective limit: unknown (the model's context window is not known); PiG's own threshold applies"), "\n"), "info"
	}
	reserve := compactlimit.ReserveTokens(os.Getenv, ctx.Cwd())
	result := compactlimit.Effective(limits, use.ContextWindow, reserve)
	if result.Native {
		return strings.Join(append(lines, "effective limit: none (the window does not exceed the reserve); PiG's own threshold applies"), "\n"), "info"
	}
	lines = append(lines, fmt.Sprintf("context window: %s, reserve: %s, automatic cap: %s (%d)",
		betterfooter.FormatTokens(use.ContextWindow), betterfooter.FormatTokens(reserve), betterfooter.FormatTokens(result.Cap), result.Cap))
	lines = append(lines, fmt.Sprintf("effective limit: %s (%d)%s", betterfooter.FormatTokens(result.Limit), result.Limit, map[bool]string{true: "", false: " - PiG's own threshold"}[result.UserSet]))
	if len(result.Clamped) > 0 {
		lines = append(lines, "clamped to the cap, so no effect on this model: "+strings.Join(result.Clamped, ", "))
	}
	return strings.Join(lines, "\n"), "info"
}

func trimPercent(value float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.4f", value), "0"), ".")
}

// endRun clears what a finished run learned about its own mid-run compacts.
func (st *state) endRun() {
	st.mu.Lock()
	st.afterCompact, st.floor = false, 0
	st.mu.Unlock()
}

// resumePrompt continues a run that a mid-run compact interrupted.
const resumePrompt = "Continue the interrupted task."

// continuesRun reports whether the turn that just ended requested tool calls, so another model request follows.
func continuesRun(data map[string]any) bool {
	message, _ := data["message"].(map[string]any)
	stop, _ := message["stopReason"].(string)
	return stop == "toolUse"
}

// trigger starts a compact when the context passes a user-set limit. The compact runs in the background; a
// failure is reported and the same context size is not retried. With resume set, a finished compact sends
// resumePrompt, because the compact aborted a run that had more work to do.
func (st *state) trigger(ctx sdk.Context, resume bool) error {
	limits, err := compactlimit.Load(compactlimit.File(os.Getenv))
	if err != nil {
		ctx.Notify("compact-settings: limit ignored, PiG's own threshold applies: "+err.Error(), "warning")
		return nil
	}
	if !limits.IsSet() {
		return nil
	}
	use, err := ctx.GetContextUsage()
	if err != nil {
		return fmt.Errorf("%s: read context usage: %w", Name, err)
	}
	if use == nil || use.Tokens == nil {
		return nil
	}
	result := compactlimit.Effective(limits, use.ContextWindow, compactlimit.ReserveTokens(os.Getenv, ctx.Cwd()))
	tokens := *use.Tokens
	st.mu.Lock()
	defer st.mu.Unlock()
	if resume && st.afterCompact {
		st.afterCompact, st.floor = false, tokens
	}
	if st.compacting || !result.ShouldCompact(tokens) || tokens == st.failedAt || (resume && st.floor > 0 && tokens <= st.floor+st.floor/4) {
		return nil
	}
	st.compacting = true
	done := make(chan struct{})
	st.done = done
	finish := func(failedAt int) {
		st.mu.Lock()
		st.compacting, st.failedAt = false, failedAt
		close(done)
		st.done = nil
		st.mu.Unlock()
	}
	// The compact aborts the run in both outcomes, so a failed compact resumes too. The size that failed is not
	// retried, which keeps a failing compact from looping.
	resumeRun := func() {
		if !resume {
			return
		}
		if err := ctx.SendUserMessage(resumePrompt, "followUp"); err != nil {
			ctx.Notify(fmt.Sprintf("compact-settings: could not resume the task after compacting: %v", err), "error")
		}
	}
	ctx.CompactWithOptions(sdk.CompactOptions{
		OnComplete: func(map[string]any) {
			finish(-1)
			if resume {
				st.mu.Lock()
				st.afterCompact, st.floor = true, 0
				st.mu.Unlock()
			}
			resumeRun()
		},
		OnError: func(err error) {
			finish(tokens)
			ctx.Notify(fmt.Sprintf("compact-settings: compacting at %s tokens failed: %v", betterfooter.FormatTokens(tokens), err), "error")
			resumeRun()
		},
	})
	return nil
}
