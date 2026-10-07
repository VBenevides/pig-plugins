package picurator

import (
	"context"
	"strconv"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/curator"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

// startupBlock returns the cached startup-decisions block, computing it on first use. The text is stored history:
// it is only ever sent as a custom message, never as system policy.
func (x *extension) startupBlock(runCtx context.Context, state *session, raw string) string {
	count := curator.StartupCount(raw)
	if count == 0 {
		return ""
	}
	state.mu.Lock()
	cached := state.startup
	state.mu.Unlock()
	if cached != nil {
		return *cached
	}
	var block string
	lines, err := curator.RecentDecisions(runCtx, x.boundOptions(state.root), count)
	if err != nil {
		x.warn("startup decisions unavailable: " + err.Error())
	} else {
		block = curator.StartupBlock(lines)
	}
	state.mu.Lock()
	state.startup = new(block)
	state.mu.Unlock()
	return block
}

// prefetchContent returns task-conditioned history for the prompt, cached per prompt.
func (x *extension) prefetchContent(runCtx context.Context, state *session, prompt, budget, engine string) string {
	state.mu.Lock()
	cached := state.prefetch
	state.mu.Unlock()
	if cached != nil && cached.prompt == prompt {
		return cached.content
	}
	entry := &prefetchEntry{prompt: prompt}
	if words := curator.PrefetchWords(prompt); len(words) > 0 {
		args := []string{"memory-search", "--query", strings.Join(words, " "), "--budget", budget, "--engine", engine}
		result, err := curator.MemoryCommand(runCtx, x.boundOptions(state.root), args)
		switch {
		case err != nil:
			x.warn("task prefetch unavailable: " + err.Error())
		case curator.HasHit(result):
			entry.content = result
		}
	}
	state.mu.Lock()
	state.prefetch = entry
	state.mu.Unlock()
	return entry.content
}

// onBeforeAgentStart adds the static recall guidance to the system prompt and, when enabled, startup decisions and
// task prefetch as an ordinary hidden custom message.
func (x *extension) onBeforeAgentStart(ctx sdk.Context, data map[string]any) (any, error) {
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
	values, _ := x.config.Effective() // a damaged settings file was reported at session start
	budget, valid, enabled := curator.PrefetchBudget(values.Prefetch)
	if !valid {
		x.warn("task prefetch disabled: " + curator.EnvPrefetchBudget + " must be 0 (off) or 64..8192")
	}
	block := x.startupBlock(runCtx, state, values.Startup)
	content := ""
	if prompt, _ := data["prompt"].(string); enabled && prompt != "" {
		content = x.prefetchContent(runCtx, state, prompt, strconv.Itoa(budget), x.prefetchEngine())
	}
	if block != "" && enabled && len(curator.History(block, content)) > budget*4 {
		x.warn("startup history omitted: combined history exceeds prefetch budget")
		block = ""
	}
	systemPrompt, _ := data["systemPrompt"].(string)
	result := map[string]any{"systemPrompt": systemPrompt + "\n\n" + recallInstructions}
	if history := curator.History(block, content); history != "" {
		result["message"] = map[string]any{"customType": "pi-curator.prefetch", "content": history, "display": false}
	}
	return result, nil
}

func (x *extension) prefetchEngine() string {
	if engine := x.getenv(curator.EnvPrefetchEngine); engine != "" {
		return engine
	}
	return curator.DefaultEngine
}
