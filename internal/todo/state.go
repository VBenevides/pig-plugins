// Package todo implements the session-snapshot contract of pi-todo@0.1.11.
package todo

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

//go:embed schema.json
var schemaJSON []byte
var schema = func() map[string]any {
	var value map[string]any
	if err := json.Unmarshal(schemaJSON, &value); err != nil {
		panic(err)
	}
	return value
}()

func Schema() map[string]any { return schema }

type Item struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}
type Params struct {
	Action string   `json:"action"`
	Text   string   `json:"text,omitempty"`
	ID     *float64 `json:"id,omitempty"`
}
type Details struct {
	Action string `json:"action"`
	Todos  []Item `json:"todos"`
	NextID int    `json:"nextId"`
	Error  string `json:"error,omitempty"`
}
type State struct {
	Todos  []Item
	NextID int
}

func New() State { return State{Todos: []Item{}, NextID: 1} }
func Decode(raw any) (Details, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return Details{}, err
	}
	var value Details
	err = json.Unmarshal(data, &value)
	return value, err
}

// Restore decodes only the latest full snapshot on the active branch.
func Restore(branch []map[string]any) (State, error) {
	var latest any
	for _, entry := range branch {
		if entry["type"] != "message" {
			continue
		}
		message, ok := entry["message"].(map[string]any)
		if !ok || message["role"] != "toolResult" || message["toolName"] != "todo" {
			continue
		}
		if message["details"] != nil {
			latest = message["details"]
		}
	}
	if latest == nil {
		return New(), nil
	}
	d, err := Decode(latest)
	if err != nil {
		return State{}, fmt.Errorf("decode todo session snapshot: %w", err)
	}
	if d.Todos == nil || d.NextID < 1 || d.NextID > 1<<53-1 {
		return State{}, fmt.Errorf("invalid todo session snapshot: missing list or invalid nextId")
	}
	seen := make(map[int]bool, len(d.Todos))
	for _, item := range d.Todos {
		if item.ID < 1 || item.ID >= d.NextID || seen[item.ID] {
			return State{}, fmt.Errorf("invalid todo session snapshot: inconsistent ID %d", item.ID)
		}
		seen[item.ID] = true
	}
	return State{Todos: d.Todos, NextID: d.NextID}, nil
}
func (s *State) snapshot(action, err string) Details {
	return Details{Action: action, Todos: slices.Clone(s.Todos), NextID: s.NextID, Error: err}
}
func number(value float64) string {
	if value == 0 {
		return "0"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err.Error()
	}
	return string(data)
}
func (s *State) Apply(p Params) (string, Details) {
	switch p.Action {
	case "list":
		text := "No todos"
		if len(s.Todos) > 0 {
			var lines strings.Builder
			for i, item := range s.Todos {
				if i > 0 {
					lines.WriteByte('\n')
				}
				check := " "
				if item.Done {
					check = "x"
				}
				fmt.Fprintf(&lines, "[%s] #%d: %s", check, item.ID, item.Text)
			}
			text = lines.String()
		}
		return text, s.snapshot("list", "")
	case "add":
		if p.Text == "" {
			return "Error: text required for add", s.snapshot("add", "text required")
		}
		if s.NextID == 1<<53-1 {
			return "Error: todo ID limit reached", s.snapshot("add", "ID limit reached")
		}
		item := Item{ID: s.NextID, Text: p.Text}
		s.NextID++
		s.Todos = append(s.Todos, item)
		return fmt.Sprintf("Added todo #%d: %s", item.ID, item.Text), s.snapshot("add", "")
	case "toggle":
		if p.ID == nil {
			return "Error: id required for toggle", s.snapshot("toggle", "id required")
		}
		for i := range s.Todos {
			item := &s.Todos[i]
			if float64(item.ID) != *p.ID {
				continue
			}
			item.Done = !item.Done
			status := "uncompleted"
			if item.Done {
				status = "completed"
			}
			return fmt.Sprintf("Todo #%d %s", item.ID, status), s.snapshot("toggle", "")
		}
		id := number(*p.ID)
		return "Todo #" + id + " not found", s.snapshot("toggle", "#"+id+" not found")
	case "clear":
		count := len(s.Todos)
		s.Todos = []Item{}
		s.NextID = 1
		return fmt.Sprintf("Cleared %d todos", count), s.snapshot("clear", "")
	default:
		return "Unknown action: " + p.Action, s.snapshot("list", "unknown action: "+p.Action)
	}
}
