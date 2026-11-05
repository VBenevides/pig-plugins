package todoext

import (
	"fmt"

	tasks "github.com/VBenevides/pig-plugins/internal/todo"
)

// Layout only: never reorder persisted phases or change task promotion.
func widgetLines(phases []tasks.Phase, _ int) []string {
	if len(phases) == 0 {
		return nil
	}
	active := tasks.ActivePhase(phases)
	if active < 0 {
		active = len(phases) - 1
	}
	var rows []string
	closed := 0
	for _, phase := range phases {
		for _, task := range phase.Tasks {
			if task.Status == "completed" || task.Status == "abandoned" || task.Status == "cancelled" {
				closed++
			}
			rows = append(rows, tasks.Marker(task.Status)+" "+tasks.SanitizeText(phase.Name)+" · "+tasks.SanitizeText(task.Content))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	header := fmt.Sprintf("Tasks · %d/%d · %s", closed, len(rows), tasks.SanitizeText(phases[active].Name))
	return append([]string{header}, rows...)
}
