package todo

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func ResolveMarkdownPath(input, cwd string) string {
	path := strings.TrimSpace(input)
	path = strings.TrimPrefix(strings.TrimPrefix(path, `"`), "'")
	path = strings.TrimSuffix(strings.TrimSuffix(path, `"`), "'")
	if path == "" {
		path = "TODO.md"
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}
func PhasesToMarkdown(phases []Phase) string {
	if len(phases) == 0 {
		return "# Tasks\n"
	}
	lines := []string{}
	for i, p := range phases {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "# "+p.Name)
		for _, task := range p.Tasks {
			marker := " "
			switch task.Status {
			case "in_progress":
				marker = "/"
			case "completed":
				marker = "x"
			case "abandoned":
				marker = "-"
			}
			lines = append(lines, "- ["+marker+"] "+task.Content)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

var markdownHeading = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)
var markdownTask = regexp.MustCompile(`^[-*+]\s*\[(.?)\]\s+(.+?)\s*$`)

func MarkdownToPhases(markdown string) ([]Phase, []string) {
	phases := []Phase{}
	errors := []string{}
	for number, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := markdownHeading.FindStringSubmatch(trimmed); match != nil {
			phases = append(phases, Phase{Name: strings.TrimSpace(match[1]), Tasks: []Task{}})
			continue
		}
		if match := markdownTask.FindStringSubmatch(trimmed); match != nil {
			if len(phases) == 0 {
				phases = append(phases, Phase{Name: "Tasks", Tasks: []Task{}})
			}
			status := ""
			switch match[1] {
			case " ", "":
				status = "pending"
			case "x", "X":
				status = "completed"
			case "/", ">":
				status = "in_progress"
			case "-", "~":
				status = "abandoned"
			}
			if status == "" {
				errors = append(errors, fmt.Sprintf("Line %d: unknown status marker \"[%s]\" (use [ ], [x], [/], [-])", number+1, match[1]))
				continue
			}
			i := len(phases) - 1
			phases[i].Tasks = append(phases[i].Tasks, Task{strings.TrimSpace(match[2]), status})
			continue
		}
		errors = append(errors, fmt.Sprintf("Line %d: unrecognized syntax \"%s\"", number+1, trimmed))
	}
	Normalize(phases)
	return phases, errors
}
