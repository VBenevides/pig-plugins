package picurator

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/curator"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

// startupBlock caches a bounded history block, never system policy. The allocation
// includes its envelope, and oversized decisions do not discard useful neighbors.
func (x *extension) startupBlock(runCtx context.Context, state *session, raw, settingsKey string, maxBytes int) string {
	count := curator.StartupCount(raw)
	if count == 0 || maxBytes <= 0 {
		return ""
	}
	key := raw + "|" + settingsKey + "|" + strconv.Itoa(maxBytes)
	state.mu.Lock()
	cached := state.startup
	state.mu.Unlock()
	if cached != nil && cached.key == key {
		return cached.content
	}
	var block string
	lines, err := curator.RecentDecisions(runCtx, x.boundOptions(state.root), count)
	if err != nil {
		x.warn("startup decisions unavailable: " + err.Error())
	} else {
		block = curator.StartupBlock(lines, maxBytes)
		if !utf8.ValidString(block) {
			x.warn("startup decisions unavailable: invalid UTF-8")
			block = ""
		}
	}
	state.mu.Lock()
	state.startup = &historyEntry{key: key, content: block}
	state.mu.Unlock()
	return block
}

// prefetchContent only issues CLI-valid calls and caches by all recall settings.
func (x *extension) prefetchContent(runCtx context.Context, state *session, prompt, settingsKey string, budget int, engine string) string {
	if budget < 64 || budget > 8192 {
		return ""
	}
	key := settingsKey + "|" + strconv.Itoa(budget) + "|" + engine
	state.mu.Lock()
	cached := state.prefetch
	state.mu.Unlock()
	if cached != nil && cached.key == key && cached.prompt == prompt {
		return cached.content
	}
	entry := &historyEntry{key: key, prompt: prompt}
	if words := curator.PrefetchWords(prompt); len(words) > 0 {
		args := []string{"memory-search", "--query", strings.Join(words, " "), "--budget", strconv.Itoa(budget), "--engine", engine}
		result, err := curator.MemoryCommand(runCtx, x.boundOptions(state.root), args)
		switch {
		case err != nil:
			x.warn("task prefetch unavailable: " + err.Error())
		case !utf8.ValidString(result) || len(result) > budget*4:
			x.warn("task prefetch unavailable: curator returned invalid or over-budget history")
		case curator.HasHit(result):
			entry.content = result
		}
	}
	state.mu.Lock()
	state.prefetch = entry
	state.mu.Unlock()
	return entry.content
}

// onBeforeAgentStart reserves the complete combined payload before either source
// runs. Task recall gets at least half the budget (and always a CLI-valid budget).
func (x *extension) onBeforeAgentStart(ctx sdk.Context, data map[string]any) (any, error) {
	if !x.enabled(ctx) {
		return nil, nil
	}
	sessionID, err := ctx.GetSessionID()
	if err != nil {
		return nil, nil
	}
	runCtx, cancel := sdkctx.Request(ctx)
	defer cancel()
	x.askConsent(ctx, runCtx, sessionID)
	state := x.current(sessionID, ctx.Cwd())
	if state == nil {
		return nil, nil
	}
	stop := context.AfterFunc(state.bg, cancel)
	defer stop()
	values, err := x.config.Effective()
	if err != nil {
		x.report(ctx, err.Error()+"; automatic recall denied")
		return nil, nil
	}
	prompt, _ := data["prompt"].(string)
	history := x.recallHistory(runCtx, state, values, prompt)
	if state.bg.Err() != nil || !x.enabled(ctx) {
		return nil, nil
	}
	systemPrompt, _ := data["systemPrompt"].(string)
	result := map[string]any{"systemPrompt": systemPrompt + "\n\n" + recallInstructions}
	if history != "" {
		result["message"] = map[string]any{"customType": "pi-curator.prefetch", "content": history, "display": false}
	}
	return result, nil
}

// recallHistory budgets both sources and the separator before allocating recall.
func (x *extension) recallHistory(runCtx context.Context, state *session, values curator.Values, prompt string) string {
	budget, valid, prefetch := curator.PrefetchBudget(values.Prefetch)
	if !valid {
		x.warn("task prefetch disabled: " + curator.EnvPrefetchBudget + " must be 0 (off) or 64..8192")
	}
	// Prefetch off disables task recall, not explicitly requested startup decisions.
	// Startup-only history still has a bounded envelope under the default total.
	if !prefetch {
		budget, _, _ = curator.PrefetchBudget(curator.DefaultPrefetch)
	}
	search := prefetch && len(curator.PrefetchWords(prompt)) > 0
	totalBytes := budget * 4
	startupBytes := totalBytes
	if search {
		startupBytes = min(totalBytes/2, totalBytes-64*4-2)
	}
	engine := x.prefetchEngine(values.Engine)
	settingsKey := values.Prefetch + "|" + values.Startup + "|" + values.Engine + "|" + engine
	block := x.startupBlock(runCtx, state, values.Startup, settingsKey, startupBytes)
	content := ""
	if search {
		remaining := totalBytes - len(block)
		if block != "" {
			remaining -= 2 // History's separator belongs to the combined budget.
		}
		content = x.prefetchContent(runCtx, state, prompt, settingsKey, remaining/4, engine)
	}
	return curator.History(block, content)
}

func (x *extension) prefetchEngine(fallback string) string {
	if engine := x.getenv(curator.EnvPrefetchEngine); engine != "" {
		return engine
	}
	return fallback
}
