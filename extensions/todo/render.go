package todoext

import (
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
	"sync"
)

func renderCall(ctx sdk.Context, args map[string]any, _ sdk.ToolRenderContext, width int) ([]string, error) {
	action, _ := args["action"].(string)
	text := "todo " + action
	if value, ok := args["text"].(string); ok && value != "" {
		text += fmt.Sprintf(" %q", value)
	}
	if value, ok := args["id"]; ok {
		text += fmt.Sprintf(" #%v", value)
	}
	return []string{ctx.UITheme().Fg("toolTitle", tui.Fit(text, width))}, nil
}
func contentText(content []map[string]any) string {
	if len(content) > 0 && content[0]["type"] == "text" {
		text, _ := content[0]["text"].(string)
		return text
	}
	return ""
}
func renderResult(ctx sdk.Context, result sdk.ToolRenderResult, options sdk.ToolRenderResultOptions, _ sdk.ToolRenderContext, width int) ([]string, error) {
	theme := ctx.UITheme()
	if result.Details == nil {
		return tui.Wrap(contentText(result.Content), width, 1024), nil
	}
	d, err := tasks.Decode(result.Details)
	if err != nil {
		return nil, err
	}
	if d.Error != "" {
		return []string{theme.Fg("error", tui.Fit("Error: "+d.Error, width))}, nil
	}
	var lines []string
	switch d.Action {
	case "list":
		if len(d.Todos) == 0 {
			return []string{theme.Fg("dim", tui.Fit("No todos", width))}, nil
		}
		lines = append(lines, theme.Fg("muted", tui.Fit(fmt.Sprintf("%d todo(s):", len(d.Todos)), width)))
		display := d.Todos
		if !options.Expanded && len(display) > 5 {
			display = display[:5]
		}
		for _, item := range display {
			check, color := "○", "muted"
			if item.Done {
				check, color = "✓", "dim"
			}
			for _, line := range tui.Wrap(fmt.Sprintf("%s #%d %s", check, item.ID, item.Text), width, 1024) {
				lines = append(lines, theme.Fg(color, line))
			}
		}
		if len(display) < len(d.Todos) {
			lines = append(lines, theme.Fg("dim", tui.Fit(fmt.Sprintf("... %d more", len(d.Todos)-len(display)), width)))
		}
	case "add":
		if len(d.Todos) == 0 {
			return nil, fmt.Errorf("added todo missing from result snapshot")
		}
		item := d.Todos[len(d.Todos)-1]
		for _, line := range tui.Wrap(fmt.Sprintf("✓ Added #%d %s", item.ID, item.Text), width, 1024) {
			lines = append(lines, theme.Fg("success", line))
		}
	case "toggle":
		for _, line := range tui.Wrap("✓ "+contentText(result.Content), width, 1024) {
			lines = append(lines, theme.Fg("success", line))
		}
	case "clear":
		lines = []string{theme.Fg("success", tui.Fit("✓ Cleared all todos", width))}
	}
	return lines, nil
}

type viewer struct {
	mu          sync.Mutex
	items       []tasks.Item
	theme       sdk.UITheme
	cachedWidth int
	cached      []string
}

func (v *viewer) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	if tui.Released(data) {
		return sdk.RemoteComponentResult{}, nil
	}
	key := tui.Key(data)
	return sdk.RemoteComponentResult{Done: key == "escape" || key == "ctrl+c"}, nil
}
func (v *viewer) Render(width int) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	width = max(1, width)
	if v.cached != nil && v.cachedWidth == width {
		return v.cached
	}
	lines := []string{"", v.theme.Fg("accent", tui.Fit("─── Todos "+strings.Repeat("─", max(0, width-10)), width)), ""}
	if len(v.items) == 0 {
		lines = append(lines, v.theme.Fg("dim", tui.Fit("  No todos yet. Ask the agent to add some!", width)))
	} else {
		done := 0
		for _, item := range v.items {
			if item.Done {
				done++
			}
		}
		lines = append(lines, v.theme.Fg("muted", tui.Fit(fmt.Sprintf("  %d/%d completed", done, len(v.items)), width)), "")
		for _, item := range v.items {
			check, color := "○", "text"
			if item.Done {
				check, color = "✓", "dim"
			}
			lines = append(lines, v.theme.Fg(color, tui.Fit(fmt.Sprintf("  %s #%d %s", check, item.ID, item.Text), width)))
		}
	}
	lines = append(lines, "", v.theme.Fg("dim", tui.Fit("  Press Escape to close", width)), "")
	v.cachedWidth = width
	v.cached = lines
	return lines
}
