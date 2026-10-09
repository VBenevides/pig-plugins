// Package todo ports pi-todotools' phased operations (MIT).
// Copyright (c) 2025 Mario Zechner; (c) 2025-2026 Can Bölük.
package todo

import (
	"fmt"
	"regexp"
	"slices"
)

type Task struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}
type Phase struct {
	Name  string `json:"name"`
	Tasks []Task `json:"tasks"`
}
type PhaseInput struct {
	Phase string   `json:"phase"`
	Items []string `json:"items"`
}
type Operation struct {
	Op    string       `json:"op"`
	List  []PhaseInput `json:"list,omitempty"`
	Task  string       `json:"task,omitempty"`
	Phase string       `json:"phase,omitempty"`
	Items []string     `json:"items,omitempty"`
}
type Completion struct {
	Phase   string `json:"phase"`
	Content string `json:"content"`
}
type PhasedDetails struct {
	Op             string       `json:"op,omitempty"`
	Phases         []Phase      `json:"phases"`
	Storage        string       `json:"storage"`
	CompletedTasks []Completion `json:"completedTasks,omitempty"`
}

func ClonePhases(phases []Phase) []Phase {
	result := make([]Phase, len(phases))
	for i, phase := range phases {
		result[i] = Phase{Name: phase.Name, Tasks: append([]Task{}, phase.Tasks...)}
	}
	return result
}
func findPhase(phases []Phase, name string) int {
	for i := range phases {
		if phases[i].Name == name {
			return i
		}
	}
	return -1
}
func findTask(phases []Phase, content string) (int, int) {
	for i := range phases {
		for j := range phases[i].Tasks {
			if phases[i].Tasks[j].Content == content {
				return i, j
			}
		}
	}
	return -1, -1
}

var taskID = regexp.MustCompile(`^task-\d+$`)

func resolveTask(phases []Phase, content string) (int, int, error) {
	if content == "" {
		return -1, -1, fmt.Errorf("Missing task content")
	}
	i, j := findTask(phases, content)
	if i >= 0 {
		return i, j, nil
	}
	if taskID.MatchString(content) {
		return -1, -1, fmt.Errorf("Task %q not found. Tasks are referenced by content, not by IDs — pass the task's full text from the previous result.", content)
	}
	hint := ""
	count := 0
	for _, p := range phases {
		count += len(p.Tasks)
	}
	if count == 0 {
		hint = " (todo list is empty — was it replaced or not yet created?)"
	}
	return -1, -1, fmt.Errorf("Task %q not found%s", content, hint)
}
func Normalize(phases []Phase) {
	active := false
	for i := range phases {
		for j := range phases[i].Tasks {
			task := &phases[i].Tasks[j]
			if task.Status == "in_progress" {
				if active {
					task.Status = "pending"
				}
				active = true
			}
		}
	}
	if active {
		return
	}
	for i := range phases {
		for j := range phases[i].Tasks {
			if phases[i].Tasks[j].Status == "pending" {
				phases[i].Tasks[j].Status = "in_progress"
				return
			}
		}
	}
}

// ApplyOperation never modifies its input, including on rejection and view.
func ApplyOperation(current []Phase, op Operation) ([]Phase, []string) {
	next := ClonePhases(current)
	errors := []string{}
	switch op.Op {
	case "view":
		return next, errors
	case "init":
		list := op.List
		if list == nil && len(op.Items) > 0 {
			name := op.Phase
			if name == "" {
				name = "Tasks"
			}
			list = []PhaseInput{{name, op.Items}}
		}
		if list == nil {
			errors = append(errors, "Missing list for init operation")
			break
		}
		next = []Phase{}
		seenPhases := map[string]bool{}
		seenTasks := map[string]bool{}
		for _, p := range list {
			if seenPhases[p.Phase] {
				errors = append(errors, fmt.Sprintf("Duplicate phase %q in init list", p.Phase))
			}
			seenPhases[p.Phase] = true
			if len(p.Items) == 0 {
				errors = append(errors, fmt.Sprintf("Phase %q has no tasks in init list", p.Phase))
			}
			phase := Phase{Name: p.Phase, Tasks: []Task{}}
			for _, content := range p.Items {
				if seenTasks[content] {
					errors = append(errors, fmt.Sprintf("Duplicate task %q in init list", content))
				}
				seenTasks[content] = true
				phase.Tasks = append(phase.Tasks, Task{content, "pending"})
			}
			next = append(next, phase)
		}
	case "append":
		if op.Phase == "" {
			errors = append(errors, "Missing phase name for append operation")
			break
		}
		if len(op.Items) == 0 {
			errors = append(errors, "Missing items for append operation")
			break
		}
		seen := map[string]bool{}
		for _, content := range op.Items {
			i, _ := findTask(next, content)
			if seen[content] || i >= 0 {
				errors = append(errors, fmt.Sprintf("Task %q already exists", content))
			}
			seen[content] = true
		}
		if len(errors) > 0 {
			break
		}
		i := findPhase(next, op.Phase)
		if i < 0 {
			next = append(next, Phase{Name: op.Phase, Tasks: []Task{}})
			i = len(next) - 1
		}
		for _, content := range op.Items {
			next[i].Tasks = append(next[i].Tasks, Task{content, "pending"})
		}
	case "start":
		i, j, err := resolveTask(next, op.Task)
		if err != nil {
			errors = append(errors, err.Error())
			break
		}
		for a := range next {
			for b := range next[a].Tasks {
				if next[a].Tasks[b].Status == "in_progress" {
					next[a].Tasks[b].Status = "pending"
				}
			}
		}
		next[i].Tasks[j].Status = "in_progress"
	case "done", "drop", "rm":
		i, j := -1, -1
		if op.Task != "" {
			var err error
			i, j, err = resolveTask(next, op.Task)
			if err != nil {
				errors = append(errors, err.Error())
				break
			}
		} else if op.Phase != "" {
			i = findPhase(next, op.Phase)
			if i < 0 {
				errors = append(errors, fmt.Sprintf("Phase %q not found", op.Phase))
				break
			}
		}
		if op.Op == "rm" {
			if j >= 0 {
				next[i].Tasks = slices.Delete(next[i].Tasks, j, j+1)
			} else {
				for a := range next {
					if i < 0 || a == i {
						next[a].Tasks = []Task{}
					}
				}
			}
			break
		}
		status := "completed"
		if op.Op == "drop" {
			status = "abandoned"
		}
		for a := range next {
			for b := range next[a].Tasks {
				if (i < 0 || a == i) && (j < 0 || b == j) {
					next[a].Tasks[b].Status = status
				}
			}
		}
	default:
		errors = append(errors, "Unknown operation: "+op.Op)
	}
	if len(errors) > 0 {
		return ClonePhases(current), errors
	}
	Normalize(next)
	return next, errors
}
func CompletionTransitions(previous, updated []Phase) []Completion {
	statuses := map[[2]string]string{}
	for _, p := range previous {
		for _, task := range p.Tasks {
			statuses[[2]string{p.Name, task.Content}] = task.Status
		}
	}
	result := []Completion{}
	for _, p := range updated {
		for _, task := range p.Tasks {
			old, ok := statuses[[2]string{p.Name, task.Content}]
			if task.Status == "completed" && ok && old != "completed" {
				result = append(result, Completion{p.Name, task.Content})
			}
		}
	}
	return result
}
