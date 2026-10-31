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
	mu                                    sync.Mutex
	enabled                               bool
	fresh                                 bool // Decide again at the next request, because a run or a session change started.
	decisions                             prune.Decisions
	lastCount                             int
	turnsSinceRefresh                     int
	sessionFile                           string
	forcePending                          bool // Manual request survives run start until the next model request.
	skillReasons                          map[string]string
	skillsRetained, skillsPruned          int
	lastRequest                           *compactlimit.RequestMeasurement
	composition                           prune.Composition
	composed                              bool
	recovery                              map[string]prune.RecoveryRef
	recoveryLedger                        map[string]bool
	recoveryReady, recoveryFailed         bool
	recoveryHandles, recoverySources      map[string]string
	errorBudgetBytes, errorExcerptBytes   int
	resultBudgetBytes, resultExcerptBytes int
	argumentHandles, argumentSources      map[string]string
	argumentBudgetBytes                   int
}

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	st := &state{}
	file := func() string { return prune.File(os.Getenv) }
	registerRecovery(e, st)

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
	e.OnEvent(compactlimit.RequestObserved, func(ctx sdk.Context, data map[string]any) (any, error) {
		measurement, err := compactlimit.ObserveRequest(ctx, data)
		if err != nil {
			return nil, err
		}
		st.mu.Lock()
		st.lastRequest = &measurement
		st.mu.Unlock()
		return nil, nil
	})

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
		pressure := overLimit(ctx)
		if err := st.refreshRecovery(ctx, pressure); err != nil {
			ctx.Notify("context-prune: recovery refresh: "+err.Error(), "error")
		}
		pruned := st.apply(messages, func() bool { return pressure })
		if pruned == nil {
			return nil, nil
		}
		if err := st.recordReductions(ctx); err != nil {
			st.mu.Lock()
			st.decisions, st.fresh, st.lastCount = nil, true, 0
			st.skillsRetained += st.skillsPruned
			st.skillsPruned = 0
			st.mu.Unlock()
			return nil, err
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
	errorBudget, errorExcerpt := prune.DefaultErrorBudgetBytes, prune.DefaultErrorExcerptBytes
	if err == nil {
		var policyErr error
		errorBudget, errorExcerpt, policyErr = prune.ErrorPolicy(file)
		if policyErr != nil {
			enabled = false
			ctx.Notify(fmt.Sprintf("%s: pruning is off: %v", Name, policyErr), "error")
		}
	}
	resultBudget, resultExcerpt := prune.DefaultResultBudgetBytes, prune.DefaultResultExcerptBytes
	if err == nil {
		var policyErr error
		resultBudget, resultExcerpt, policyErr = prune.ResultPolicy(file)
		if policyErr != nil {
			enabled = false
			ctx.Notify(fmt.Sprintf("%s: pruning is off: %v", Name, policyErr), "error")
		}
	}
	argumentBudget := prune.DefaultArgumentBudgetBytes
	if err == nil {
		var policyErr error
		argumentBudget, policyErr = prune.ArgumentBudget(file)
		if policyErr != nil {
			enabled = false
			ctx.Notify(fmt.Sprintf("%s: pruning is off: %v", Name, policyErr), "error")
		}
	}
	reasons := map[string]string{}
	var entries []map[string]any
	maxAge, ageErr := prune.SkillMaxAge(file)
	if ageErr != nil {
		ctx.Notify(fmt.Sprintf("context-prune: skill pruning is off: %v", ageErr), "error")
	} else {
		var readErr error
		entries, readErr = ctx.SessionManager().GetBranch(nil)
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
	refs, recoveryErr := activeRecoveryReferences(ctx, sessionFile)
	if recoveryErr != nil {
		ctx.Notify("context-prune: recovery storage unavailable: "+recoveryErr.Error(), "error")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.enabled, st.fresh = enabled, true
	st.skillReasons = reasons
	st.sessionFile, st.turnsSinceRefresh = sessionFile, 0
	st.setRecoveryLocked(refs)
	st.errorBudgetBytes, st.errorExcerptBytes = errorBudget, errorExcerpt
	st.resultBudgetBytes, st.resultExcerptBytes = resultBudget, resultExcerpt
	st.argumentBudgetBytes = argumentBudget
	st.recoveryLedger = recoveryLedger(entries)
	st.recoveryReady, st.recoveryFailed = refs != nil, recoveryErr != nil
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
	st.decisions.DiscardChangedSources(messages)
	settings := prune.DefaultSettings
	settings.SkillReasons = st.skillReasons
	if st.errorBudgetBytes > 0 && st.errorExcerptBytes > 0 {
		settings.HistoricalErrorBytes, settings.ErrorExcerptBytes = st.errorBudgetBytes, st.errorExcerptBytes
	}
	if st.resultBudgetBytes > 0 {
		settings.HistoricalResultBytes, settings.ResultExcerptBytes = st.resultBudgetBytes, st.resultExcerptBytes
	}
	if st.recoveryReady {
		settings.SessionFile = st.sessionFile
	}
	settings.RecoveryHandles, settings.RecoverySources = st.recoveryHandles, st.recoverySources
	settings.ArgumentHandles, settings.ArgumentSources = st.argumentHandles, st.argumentSources
	if st.argumentBudgetBytes > 0 {
		settings.HistoricalArgumentBytes = st.argumentBudgetBytes
	}
	force := st.forcePending
	st.forcePending = false
	if force {
		settings.MinSavingChars = 1
	}
	if st.fresh {
		settings.Pressure = overLimit()
		st.decisions = prune.Decide(messages, settings)
		st.fresh = false
		st.turnsSinceRefresh = 0
	} else if pressure := overLimit(); force || st.turnsSinceRefresh >= RefreshEveryTurns || pressure {
		settings.Pressure = pressure
		current, _ := prune.Apply(messages, st.decisions)
		settings.RecoverySources = st.decisions.ViewSources(messages, settings.RecoverySources)
		more := prune.Decide(current, settings)
		if st.decisions == nil {
			st.decisions = prune.Decisions{}
		}
		st.decisions.MergeView(more, messages)
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
	view := messages
	if changed > 0 {
		view = pruned
	}
	st.composition, st.composed = prune.Compose(view, max(0, len(view)-prune.DefaultSettings.KeepRecent)), true
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
	return fmt.Sprintf("%s: now: prepared %d pruned messages, roughly %d tokens saved. Applies to the next model request; no LLM call. Recent results, signed content, and images remain protected. Old failures keep their status and recovery handles.", Name, st.lastCount, st.decisions.Saved()/prune.CharsPerToken), "info"
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
	lines := []string{
		fmt.Sprintf("%s: %s", Name, state),
		fmt.Sprintf("current run: %d messages pruned in the last request, about %d characters (roughly %d tokens, an estimate) left out of each request", st.lastCount, chars, chars/prune.CharsPerToken),
		fmt.Sprintf("policy: refresh every %d tool-use turns or under pressure; protect %d tool-call groups and %d newest messages; require %d new characters (roughly %d tokens) per batch", RefreshEveryTurns, prune.DefaultSettings.KeepRecentGroups, prune.DefaultSettings.KeepRecent, prune.DefaultSettings.MinSavingChars, prune.DefaultSettings.MinSavingChars/prune.CharsPerToken),
		fmt.Sprintf("budgets: old failures %d bytes, old results %d bytes, old edit and write payloads %d bytes; batches reduce to 75 percent of a budget", st.errorBudgetBytes, st.resultBudgetBytes, st.argumentBudgetBytes),
		fmt.Sprintf("skill bodies: %d retained, %d pruned; only explicitly finished or released-and-expired bodies are eligible; recent messages and minimum savings still apply", st.skillsRetained, st.skillsPruned),
		"folding: off; the extension SDK has no trusted endpoint and credential identity yet",
	}
	if st.composed {
		c := st.composition
		lines = append(lines, fmt.Sprintf("composition (message bytes in the last view, not tokens; fixed instructions and tool schemas excluded): user %d, assistant %d, call arguments %d, results %d, failures %d, recovery notices %d, images %d", c.UserText, c.AssistantText, c.CallArguments, c.ResultText, c.FailureText, c.RecoveryNotices, c.Media),
			fmt.Sprintf("results: %d reduced of %d; %d bytes in the newest %d messages stay whole", c.ResultsReduced, c.ResultsTotal, c.ProtectedResults, prune.DefaultSettings.KeepRecent))
	}
	if st.lastRequest != nil {
		confidence := "text-only byte estimate; not a billed-token count"
		if st.lastRequest.UnboundedMedia {
			confidence = "media or opaque state has unknown token cost; the estimate is a lower bound"
		}
		lines = append(lines, fmt.Sprintf("last request: about %d tokens plus %d reserved for output; %s", st.lastRequest.Tokens, st.lastRequest.OutputReserve, confidence))
	} else {
		lines = append(lines, "last request: not measured; this host did not report a request observation")
	}
	if fallback := st.fallbackReasonLocked(saved); fallback != "" {
		lines = append(lines, "fallback: "+fallback)
	}
	return strings.Join(lines, "\n"), "info"
}

// fallbackReasonLocked says why reduction is limited. Each reason names an action.
func (st *state) fallbackReasonLocked(saved bool) string {
	switch {
	case !saved:
		return "pruning is off. Use /context-prune on. Host compaction still works."
	case st.recoveryFailed:
		return "saved-history recovery failed, so old results keep their full text. Check the session file, then start a new run."
	case !st.recoveryReady:
		return "saved-history recovery is not ready yet, so age-based pruning waits. Send a message to start a run."
	}
	return ""
}
