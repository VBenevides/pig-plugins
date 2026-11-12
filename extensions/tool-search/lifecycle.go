package toolsearch

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

// discoveries owns only activations reported by tool_search. Other activation sources are not reset.
type discoveries struct {
	mu     sync.Mutex
	loaded map[string]bool
}

func (d *discoveries) result(ctx sdk.Context, data map[string]any) (any, error) {
	if data["toolName"] != "tool_search" || data["isError"] == true {
		return nil, nil
	}
	details, _ := data["details"].(map[string]any)
	raw, _ := details["loaded"].([]any)
	if len(raw) == 0 {
		return nil, nil
	}
	cfg, err := toolsettings.LoadEffective(toolsettings.Path())
	if err != nil {
		return nil, fmt.Errorf("tool-search: read permissions: %w", err)
	}
	active, err := ctx.GetActiveTools()
	if err != nil {
		return nil, fmt.Errorf("tool-search: read active tools: %w", err)
	}
	var allowed []string
	denied := map[string]bool{}
	d.mu.Lock()
	if d.loaded == nil {
		d.loaded = map[string]bool{}
	}
	for _, value := range raw {
		name, ok := value.(string)
		if !ok {
			continue
		}
		if !cfg.ToolEnabled(name) {
			denied[name] = true
			continue
		}
		d.loaded[name] = true
		allowed = append(allowed, name)
	}
	d.mu.Unlock()
	if len(denied) == 0 {
		return nil, nil
	}
	ctx.SetActiveTools(slices.DeleteFunc(active, func(name string) bool { return denied[name] }))
	text := "No permitted tools matched."
	if len(allowed) > 0 {
		text = "Loaded tools: " + strings.Join(allowed, ", ")
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "details": map[string]any{"loaded": allowed}}, nil
}

func (d *discoveries) settle(ctx sdk.Context, _ map[string]any) (any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.loaded) == 0 {
		return nil, nil
	}
	active, err := ctx.GetActiveTools()
	if err != nil {
		return nil, fmt.Errorf("tool-search: release discovered tools: %w", err)
	}
	ctx.SetActiveTools(slices.DeleteFunc(active, func(name string) bool { return d.loaded[name] }))
	d.loaded = nil
	return nil, nil
}

func boundSearch(_ sdk.Context, data map[string]any) (any, error) {
	if data["toolName"] != "tool_search" {
		return nil, nil
	}
	input, _ := data["input"].(map[string]any)
	query, _ := input["query"].(string)
	limit, _ := input["limit"].(float64)
	if len(query) > 4096 || limit > 20 {
		return map[string]any{"block": true, "reason": "tool-search: use at most 4096 query bytes and a limit of 20"}, nil
	}
	return nil, nil
}
