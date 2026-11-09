package cursor

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

// piUsage maps Cursor usage to PiG's message usage. PiG adds input, output,
// cacheRead and cacheWrite, so input must exclude cached tokens. Cursor's
// inputTokens includes them (see UncachedInput).
func piUsage(u cursorbridge.TokenUsage) map[string]any {
	input, output := int64(u.UncachedInput()), int64(u.OutputTokens)
	read, write := int64(u.CacheReadTokens), int64(u.CacheWriteTokens)
	return map[string]any{"input": input, "output": output, "cacheRead": read, "cacheWrite": write, "totalTokens": input + output + read + write}
}

// modelUsage is what one model used since the session started.
type modelUsage struct {
	turns, calls int
	usage        cursorbridge.TokenUsage
}

// usageTally keeps token totals for the /cursor-usage command. It is memory only
// and starts empty with each session.
type usageTally struct {
	mu      sync.Mutex
	byModel map[string]*modelUsage
}

func (t *usageTally) reset() {
	t.mu.Lock()
	t.byModel = nil
	t.mu.Unlock()
}

// add records one PiG turn. A turn that reported no usage still counts as a turn.
func (t *usageTally) add(model string, outcome cursorbridge.Outcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byModel == nil {
		t.byModel = map[string]*modelUsage{}
	}
	m := t.byModel[model]
	if m == nil {
		m = &modelUsage{}
		t.byModel[model] = m
	}
	m.turns++
	m.calls += len(outcome.ModelCalls)
	if outcome.Usage != nil {
		m.usage = m.usage.Add(*outcome.Usage)
	}
}

func share(part, whole cursorbridge.Int64) string {
	if whole <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(part)/float64(whole))
}

// report formats the totals. Reasoning tokens are part of output.
func (t *usageTally) report() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.byModel) == 0 {
		return "No Cursor usage in this session yet."
	}
	models := make([]string, 0, len(t.byModel))
	for model := range t.byModel {
		models = append(models, model)
	}
	sort.Strings(models)
	var all cursorbridge.TokenUsage
	var lines []string
	for _, model := range models {
		m := t.byModel[model]
		all = all.Add(m.usage)
		lines = append(lines, "Cursor "+model, describe(m))
	}
	if len(models) > 1 {
		lines = append(lines, "All Cursor models", describe(&modelUsage{usage: all}))
	}
	lines = append(lines, "", "Input includes cached tokens. Uncached = input - cache read - cache write.")
	return strings.Join(lines, "\n")
}

func describe(m *modelUsage) string {
	u := m.usage
	text := fmt.Sprintf("  input %d (cached %d = %s, uncached %d), output %d (reasoning %d)",
		u.InputTokens, u.CacheReadTokens+u.CacheWriteTokens, share(u.CacheReadTokens+u.CacheWriteTokens, u.InputTokens), u.UncachedInput(), u.OutputTokens, u.ReasoningTokens)
	if m.turns > 0 {
		text += fmt.Sprintf("\n  %d turns", m.turns)
		if m.calls > 0 {
			text += fmt.Sprintf(", %d model calls", m.calls)
		}
	}
	return text
}

func modelKey(modelID, level string) string {
	if level == "" {
		return modelID
	}
	return modelID + " (" + level + ")"
}
