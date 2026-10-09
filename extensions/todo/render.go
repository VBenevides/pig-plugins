package todoext

import (
	"encoding/json"
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
)

func countLabel(count int, singular string) string {
	if count != 1 {
		singular += "s"
	}
	return fmt.Sprintf("%d %s", count, singular)
}
func callLabel(op tasks.Operation) string {
	target := op.Task
	if target == "" {
		target = op.Phase
	}
	target = tasks.SanitizeText(target)
	switch op.Op {
	case "init":
		phases, count := 0, len(op.Items)
		if count > 0 {
			phases = 1
		}
		if op.List != nil {
			phases = len(op.List)
			count = 0
			for _, p := range op.List {
				count += len(p.Items)
			}
		}
		return "todo init (" + countLabel(phases, "phase") + ", " + countLabel(count, "task") + ")"
	case "append":
		phase := tasks.SanitizeText(op.Phase)
		if phase == "" {
			phase = "(missing phase)"
		}
		return "todo append: " + phase + " (" + countLabel(len(op.Items), "item") + ")"
	case "start", "done", "drop":
		if target == "" {
			target = "(missing target)"
		}
		return "todo " + op.Op + ": " + target
	case "rm":
		if target == "" {
			target = "all"
		}
		return "todo rm: " + target
	default:
		return "todo " + tasks.SanitizeText(op.Op)
	}
}
func renderCall(ctx sdk.Context, args map[string]any, _ sdk.ToolRenderContext, width int) ([]string, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	var op tasks.Operation
	if err = json.Unmarshal(data, &op); err != nil {
		return nil, err
	}
	theme := ctx.UITheme()
	return []string{theme.Fg("toolTitle", theme.Bold(tui.Fit(callLabel(op), max(1, width))))}, nil
}
func contentText(content []map[string]any) string {
	texts := []string{}
	for _, block := range content {
		if block["type"] == "text" {
			if text, ok := block["text"].(string); ok {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}
func roman(index int) string {
	result := ""
	for _, pair := range []struct {
		value  int
		symbol string
	}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for index >= pair.value {
			result += pair.symbol
			index -= pair.value
		}
	}
	return result
}
func phaseLines(phases []tasks.Phase, completed []tasks.Completion, expanded bool, op tasks.Operation, theme sdk.UITheme, width int) []string {
	visible := []tasks.Phase{}
	for _, p := range phases {
		if len(p.Tasks) > 0 {
			visible = append(visible, p)
		}
	}
	touched := map[string]bool{}
	if i := tasks.ActivePhase(visible); i >= 0 {
		touched[visible[i].Name] = true
	}
	for _, c := range completed {
		touched[c.Phase] = true
	}
	for _, p := range visible {
		if op.Op == "init" || p.Name == op.Phase {
			touched[p.Name] = true
		}
		for _, task := range p.Tasks {
			if task.Content == op.Task {
				touched[p.Name] = true
			}
		}
	}
	lines := []string{}
	width = max(1, width)
	add := func(text, color string, bold, strike bool) {
		if len(lines) >= 1024 {
			return
		}
		for _, line := range tui.Wrap(text, width, 1024-len(lines)) {
			if bold {
				line = theme.Bold(line)
			}
			if strike {
				line = theme.Strikethrough(line)
			}
			if color != "" {
				line = theme.Fg(color, line)
			}
			lines = append(lines, line)
		}
	}
	for i, p := range visible {
		title := roman(i+1) + ". " + tasks.SanitizeText(p.Name)
		if !expanded && len(visible) > 1 && len(touched) > 0 && !touched[p.Name] {
			closed := 0
			for _, task := range p.Tasks {
				if task.Status == "completed" || task.Status == "abandoned" {
					closed++
				}
			}
			add(fmt.Sprintf("%s — %d/%d done", title, closed, len(p.Tasks)), "dim", false, false)
			continue
		}
		add(title, "accent", true, false)
		for _, task := range p.Tasks {
			color := ""
			if task.Status == "in_progress" {
				color = "accent"
			} else if task.Status == "completed" || task.Status == "abandoned" {
				color = "dim"
			}
			add("  "+tasks.Marker(task.Status)+" "+tasks.SanitizeText(task.Content), color, task.Status == "in_progress", task.Status == "completed")
		}
	}
	return lines
}
func renderResult(ctx sdk.Context, result sdk.ToolRenderResult, options sdk.ToolRenderResultOptions, render sdk.ToolRenderContext, width int) ([]string, error) {
	return resultLines(result, options, render, ctx.UITheme(), width)
}

func resultLines(result sdk.ToolRenderResult, options sdk.ToolRenderResultOptions, render sdk.ToolRenderContext, theme sdk.UITheme, width int) ([]string, error) {
	fallback := func(color string) []string {
		text := contentText(result.Content)
		if text == "" {
			text = "Todo list is empty."
		}
		lines := tui.Wrap(text, max(1, width), 1024)
		for i, line := range lines {
			lines[i] = theme.Fg(color, line)
		}
		return lines
	}
	if render.IsError {
		return fallback("toolOutput"), nil
	}
	if result.Details == nil {
		return fallback("toolOutput"), nil
	}
	details, err := tasks.Decode(result.Details)
	if err != nil {
		return nil, err
	}
	var op tasks.Operation
	data, err := json.Marshal(render.Args)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &op); err != nil {
		return nil, err
	}
	if op.Op == "" {
		op.Op = details.Op
	}
	lines := phaseLines(details.Phases, details.CompletedTasks, options.Expanded, op, theme, width)
	if len(lines) == 0 {
		lines = fallback("toolOutput")
	}
	return lines, nil
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
	width = max(1, width)
	lines := []string{v.theme.Fg("accent", tui.Fit("Todos", width))}
	total, closed := 0, 0
	for _, p := range v.phases {
		for _, task := range p.Tasks {
			total++
			if task.Status == "completed" || task.Status == "abandoned" {
				closed++
			}
		}
	}
	lines = append(lines, tui.Fit(fmt.Sprintf("%d/%d done", closed, total), width))
	lines = append(lines, phaseLines(v.phases, nil, true, tasks.Operation{Op: "view"}, v.theme, width)...)
	if total == 0 {
		lines = append(lines, tui.Fit("Todo list is empty.", width))
	}
	return append(lines, "", tui.Fit("Press Escape to close", width))
}

func syncWidget(ctx sdk.Context, phases []tasks.Phase) error {
	if !ctx.HasUI() {
		return nil
	}
	// PiG hosts string-array widgets above the editor, including wrapping,
	// resize handling, and the same ten-row cap as Pi. nil clears the widget.
	if err := ctx.SetWidget("todo-sidebar", tasks.WidgetLines(phases)); err != nil {
		return fmt.Errorf("refresh todo widget: %w", err)
	}
	return nil
}
