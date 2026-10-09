package todo

import (
	"fmt"
	"strings"
)

// FormatSummary preserves upstream model-visible text, including verbatim refs.
func FormatSummary(phases []Phase, errors []string, readOnly bool) string {
	total, closed, open, current := 0, 0, 0, -1
	for i, p := range phases {
		for _, task := range p.Tasks {
			total++
			if task.Status == "completed" || task.Status == "abandoned" {
				closed++
			} else {
				open++
				if current < 0 {
					current = i
				}
			}
		}
	}
	if total == 0 {
		if len(errors) > 0 {
			return "Errors: " + strings.Join(errors, "; ")
		}
		if readOnly {
			return "Todo list is empty."
		}
		return "Todo list cleared."
	}
	if current < 0 {
		current = len(phases) - 1
	}
	lines := []string{}
	if len(errors) > 0 {
		lines = append(lines, "Errors: "+strings.Join(errors, "; "))
	}
	if open == 0 {
		lines = append(lines, "Remaining items: none.")
	} else {
		lines = append(lines, fmt.Sprintf("Remaining items (%d):", open))
		for _, p := range phases {
			for _, task := range p.Tasks {
				if task.Status == "pending" || task.Status == "in_progress" {
					lines = append(lines, fmt.Sprintf("  - %s [%s] (%s)", task.Content, task.Status, p.Name))
				}
			}
		}
	}
	lines = append(lines, fmt.Sprintf("Overall: %d/%d done, %d open.", closed, total, open))
	done := 0
	for _, task := range phases[current].Tasks {
		if task.Status == "completed" || task.Status == "abandoned" {
			done++
		}
	}
	suffix := "."
	for i, p := range phases {
		if i <= current {
			continue
		}
		for _, task := range p.Tasks {
			if task.Status == "completed" || task.Status == "abandoned" {
				suffix = " — earliest phase with open tasks; the in-progress pointer auto-advances to the earliest open task on each completion, so it can sit behind out-of-order work (nothing was un-completed)."
			}
		}
	}
	lines = append(lines, fmt.Sprintf("Active phase %d/%d \"%s\" (%d/%d)%s", current+1, len(phases), phases[current].Name, done, len(phases[current].Tasks), suffix))
	for _, p := range phases {
		lines = append(lines, "  "+p.Name+":")
		for _, task := range p.Tasks {
			checkbox, tag := "[ ]", ""
			if task.Status == "completed" {
				checkbox = "[X]"
			}
			if task.Status == "in_progress" {
				tag = " (in progress)"
			} else if task.Status == "abandoned" {
				tag = " (dropped)"
			}
			lines = append(lines, "    - "+checkbox+" "+task.Content+tag)
		}
	}
	return strings.Join(lines, "\n")
}
