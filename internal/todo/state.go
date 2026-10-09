// Package todo implements pi-todotools' branch-local phased session state.
package todo

import (
	"encoding/json"
	"fmt"
)

const StateEntryType = "sanepi.todo-state"

type StateEntry struct {
	Schema string  `json:"schema"`
	Phases []Phase `json:"phases"`
}

func Decode(raw any) (PhasedDetails, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return PhasedDetails{}, err
	}
	var value PhasedDetails
	err = json.Unmarshal(data, &value)
	return value, err
}

// Restore reads snapshots in branch order, matching upstream's latest valid
// snapshot policy. Malformed entries are reported instead of silently ignored.
func Restore(branch []map[string]any) ([]Phase, []string) {
	phases := []Phase{}
	warnings := []string{}
	for i, entry := range branch {
		var payload any
		if entry["type"] == "custom" && entry["customType"] == StateEntryType {
			payload = entry["data"]
		} else if entry["type"] == "message" {
			message, ok := entry["message"].(map[string]any)
			if !ok || message["role"] != "toolResult" || (message["toolName"] != "todo" && message["toolName"] != "todowrite") {
				continue
			}
			payload = message["details"]
		} else {
			continue
		}
		if payload == nil {
			continue
		}
		parsed, err := parsePayload(payload)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("todo snapshot at branch entry %d: %v", i, err))
			continue
		}
		phases = parsed
	}
	return ClonePhases(phases), warnings
}

func parsePayload(raw any) ([]Phase, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if value, ok := payload["phases"]; ok {
		var phases []Phase
		if err = json.Unmarshal(value, &phases); err != nil {
			return nil, err
		}
		if phases == nil {
			return nil, fmt.Errorf("missing phases array")
		}
		// Decode required string fields separately so missing fields are not accepted
		// as empty strings. Empty strings themselves are allowed by upstream.
		var required []struct {
			Name  *string
			Tasks []struct {
				Content *string
				Status  *string
			}
		}
		if err = json.Unmarshal(value, &required); err != nil {
			return nil, err
		}
		for i, p := range required {
			if p.Name == nil || p.Tasks == nil {
				return nil, fmt.Errorf("invalid phase %d", i)
			}
			for j, task := range p.Tasks {
				if task.Content == nil || task.Status == nil {
					return nil, fmt.Errorf("invalid task %d in phase %d", j, i)
				}
				status := *task.Status
				if status == "cancelled" {
					status = "abandoned"
				}
				if status != "pending" && status != "in_progress" && status != "completed" && status != "abandoned" {
					return nil, fmt.Errorf("invalid task status %q", status)
				}
				phases[i].Tasks[j].Status = status
			}
		}
		return ClonePhases(phases), nil
	}
	if string(payload["schema"]) == `"v2"` {
		return nil, fmt.Errorf("v2 snapshot missing phases")
	}
	value, ok := payload["todos"]
	if !ok {
		return nil, fmt.Errorf("unrecognized snapshot")
	}
	var items []struct {
		Content *string
		Status  string
		Text    *string
		Done    *bool
		ID      *int
	}
	if err = json.Unmarshal(value, &items); err != nil {
		return nil, err
	}
	if items == nil {
		return nil, fmt.Errorf("missing todos array")
	}
	tasks := []Task{}
	for i, item := range items {
		content := ""
		status := item.Status
		if item.Content != nil {
			content = *item.Content
			if status == "cancelled" {
				status = "abandoned"
			}
			if status != "pending" && status != "in_progress" && status != "completed" && status != "abandoned" {
				status = "pending"
			}
		} else if item.Text != nil && item.Done != nil && item.ID != nil {
			content = *item.Text
			status = "pending"
			if *item.Done {
				status = "completed"
			}
		} else {
			return nil, fmt.Errorf("invalid legacy task %d", i)
		}
		tasks = append(tasks, Task{content, status})
	}
	return []Phase{{Name: "Tasks", Tasks: tasks}}, nil
}

// CommitOperation writes before publishing memory state. Failed validation and
// view are read-only; a persistence error leaves the caller's state untouched.
func CommitOperation(current []Phase, op Operation, persist func(StateEntry) error) (PhasedDetails, []string, error) {
	next, errors := ApplyOperation(current, op)
	details := PhasedDetails{Op: op.Op, Phases: ClonePhases(next), Storage: "session"}
	if op.Op == "view" || len(errors) > 0 {
		return details, errors, nil
	}
	if err := persist(StateEntry{Schema: "v2", Phases: ClonePhases(next)}); err != nil {
		return PhasedDetails{}, nil, fmt.Errorf("persist todo %s: %w", op.Op, err)
	}
	details.CompletedTasks = CompletionTransitions(current, next)
	return details, errors, nil
}
