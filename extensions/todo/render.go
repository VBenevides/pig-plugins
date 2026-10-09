package todoext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
)

func renderCall(ctx sdk.Context, args map[string]any, _ sdk.ToolRenderContext, width int) ([]string, error) {
	op, _ := args["op"].(string)
	text := "todo " + op
	for _, key := range []string{"task", "phase"} {
		if value, ok := args[key].(string); ok {
			text += " " + value
			break
		}
	}
	return []string{ctx.UITheme().Fg("toolTitle", tui.Fit(text, width))}, nil
}
func contentText(content []map[string]any) string {
	if len(content) > 0 {
		text, _ := content[0]["text"].(string)
		return text
	}
	return ""
}
func renderResult(_ sdk.Context, result sdk.ToolRenderResult, _ sdk.ToolRenderResultOptions, _ sdk.ToolRenderContext, width int) ([]string, error) {
	return tui.Wrap(contentText(result.Content), width, 1024), nil
}

type viewer struct {
	phases []tasks.Phase
	theme  sdk.UITheme
}

func (v *viewer) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	if tui.Released(data) {
		return sdk.RemoteComponentResult{}, nil
	}
	key := tui.Key(data)
	return sdk.RemoteComponentResult{Done: key == "escape" || key == "ctrl+c"}, nil
}
func (v *viewer) Render(width int) []string {
	lines := tui.Wrap(tasks.FormatSummary(v.phases, nil, true), max(1, width), 1024)
	lines = append(lines, "", tui.Fit("Press Escape to close", max(1, width)))
	return lines
}
