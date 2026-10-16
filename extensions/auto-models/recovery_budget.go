package automodels

import (
	"fmt"
	"strings"
)

// taskInput starts a recovery budget only for a fresh, explicit idle input.
// Extension continuations, queued follow-ups, steering, and unknown provenance
// belong to the current operation. A failed idle query cannot grant a budget.
func (x *extension) taskInput(data map[string]any, isIdle func() (bool, error)) (any, error) {
	source, _ := data["source"].(string)
	if source != "interactive" && source != "rpc" {
		return nil, nil
	}
	if behavior, exists := data["streamingBehavior"]; exists && behavior != "" {
		return nil, nil
	}
	text, _ := data["text"].(string)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	idle, err := isIdle()
	if err != nil {
		return nil, fmt.Errorf("auto-models: establish new task recovery budget: %w", err)
	}
	if !idle {
		return nil, nil
	}
	x.mu.Lock()
	x.retryUsed, x.retryPending = false, false
	x.mu.Unlock()
	return nil, nil
}
