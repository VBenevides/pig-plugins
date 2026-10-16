package todoext

import (
	"fmt"

	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
)

// Layout only: never reorder persisted phases or change task promotion.
func widgetLines(phases []tasks.Phase, width int) []string {
	if len(phases) == 0 {
		return nil
	}
	active := tasks.ActivePhase(phases)
	if active < 0 {
		active = len(phases) - 1
	}
	var open, closed []string
	for _, task := range phases[active].Tasks {
		line := tasks.Marker(task.Status) + " " + tasks.SanitizeText(task.Content)
		switch task.Status {
		case "completed", "abandoned", "cancelled":
			closed = append(closed, line)
		default:
			open = append(open, line)
		}
	}
	lines := []string{tui.Fit("Todo — "+tasks.SanitizeText(phases[active].Name), width)}
	if width < 72 {
		lines = append(lines, tui.Fit(fmt.Sprintf("TODO / IN PROGRESS (%d)", len(open)), width))
		for _, line := range open {
			lines = append(lines, tui.Fit(line, width))
		}
		if len(closed) > 0 {
			lines = append(lines, tui.Fit(fmt.Sprintf("DONE / CLOSED (%d)", len(closed)), width))
			for _, line := range closed {
				lines = append(lines, tui.Fit(line, width))
			}
		}
		return lines
	}
	left := (width - 3) / 2
	right := width - 3 - left
	lines = append(lines, tui.Pad(tui.Fit(fmt.Sprintf("TODO / IN PROGRESS (%d)", len(open)), left), left)+" │ "+tui.Fit(fmt.Sprintf("DONE / CLOSED (%d)", len(closed)), right))
	for i := 0; i < max(len(open), len(closed)); i++ {
		a, b := "", ""
		if i < len(open) {
			a = open[i]
		}
		if i < len(closed) {
			b = closed[i]
		}
		lines = append(lines, tui.Pad(tui.Fit(a, left), left)+" │ "+tui.Fit(b, right))
	}
	return lines
}
