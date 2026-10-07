package todo

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestUpstreamActionAndBranchParity(t *testing.T) {
	var fixture struct {
		Runs []struct {
			Name    string
			Branch  []map[string]any
			Actions []struct {
				Params   Params
				Response struct {
					Content []struct{ Text string }
					Details Details
				}
			}
		}
	}
	data, err := os.ReadFile("../../testfixtures/todo.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, run := range fixture.Runs {
		t.Run(run.Name, func(t *testing.T) {
			state, err := Restore(run.Branch)
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range run.Actions {
				text, details := state.Apply(step.Params)
				if text != step.Response.Content[0].Text || !reflect.DeepEqual(details, step.Response.Details) {
					t.Fatalf("%#v: got %q %#v want %#v", step.Params, text, details, step.Response)
				}
			}
		})
	}
}
func TestMalformedLatestSnapshotFails(t *testing.T) {
	entry := func(details any) map[string]any {
		return map[string]any{"type": "message", "message": map[string]any{"role": "toolResult", "toolName": "todo", "details": details}}
	}
	for _, details := range []any{map[string]any{}, map[string]any{"todos": nil, "nextId": 1}, map[string]any{"todos": []any{map[string]any{"id": 2, "text": "item", "done": false}}, "nextId": 2}, map[string]any{"todos": []any{}, "nextId": float64(1 << 53)}} {
		if _, err := Restore([]map[string]any{entry(Details{Action: "list", Todos: []Item{}, NextID: 1}), entry(details)}); err == nil {
			t.Fatalf("accepted malformed latest snapshot %#v", details)
		}
	}
}
func TestCounterPrecisionLimitPreservesState(t *testing.T) {
	s := New()
	s.NextID = 1<<53 - 1
	_, details := s.Apply(Params{Action: "add", Text: "item"})
	if details.Error == "" || s.NextID != 1<<53-1 || len(s.Todos) != 0 {
		t.Fatalf("counter overflow changed state: %#v", s)
	}
}
