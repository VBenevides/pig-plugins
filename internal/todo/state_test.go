package todo

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func custom(payload any) map[string]any {
	return map[string]any{"type": "custom", "customType": StateEntryType, "data": payload}
}
func toolSnapshot(name string, payload any) map[string]any {
	return map[string]any{"type": "message", "message": map[string]any{"role": "toolResult", "toolName": name, "details": payload}}
}

func TestLegacyLocalFixturesMigrate(t *testing.T) {
	// Keep the captured pi-todo fixtures as migration evidence, not an old API test.
	data, err := os.ReadFile("../../testfixtures/todo.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Runs []struct {
			Name    string
			Branch  []map[string]any
			Actions []struct {
				Response struct{ Details json.RawMessage }
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, run := range fixture.Runs {
		for _, action := range run.Actions {
			var old struct {
				Todos []struct {
					Text string
					Done bool
				}
			}
			if err = json.Unmarshal(action.Response.Details, &old); err != nil {
				t.Fatal(err)
			}
			migrated, err := parsePayload(action.Response.Details)
			if err != nil {
				t.Fatalf("%s: %v", run.Name, err)
			}
			if len(migrated) != 1 || len(migrated[0].Tasks) != len(old.Todos) {
				t.Fatal(migrated)
			}
			for i, item := range old.Todos {
				status := "pending"
				if item.Done {
					status = "completed"
				}
				if migrated[0].Tasks[i] != (Task{item.Text, status}) {
					t.Fatal(migrated)
				}
			}
			count++
		}
		_, warnings := Restore(run.Branch)
		if len(warnings) > 0 {
			t.Fatalf("%s: %v", run.Name, warnings)
		}
	}
	if count == 0 {
		t.Fatal("empty historical fixture")
	}
	local := map[string]any{"action": "list", "todos": []any{map[string]any{"id": 1, "text": "open", "done": false}, map[string]any{"id": 2, "text": "closed", "done": true}}, "nextId": 3}
	phases, warnings := Restore([]map[string]any{toolSnapshot("todo", local)})
	want := []Phase{{"Tasks", []Task{{"open", "pending"}, {"closed", "completed"}}}}
	if len(warnings) > 0 || !reflect.DeepEqual(phases, want) {
		t.Fatal(phases, warnings)
	}
}

func TestRestoreSourceOrderAndLegacyStatuses(t *testing.T) {
	legacy := map[string]any{"todos": []any{map[string]any{"content": "blocked", "status": "blocked", "priority": "high"}, map[string]any{"content": "cancelled", "status": "cancelled"}}}
	first := []Phase{{"Tasks", []Task{{"blocked", "pending"}, {"cancelled", "abandoned"}}}}
	phases, warnings := Restore([]map[string]any{custom(legacy)})
	if len(warnings) > 0 || !reflect.DeepEqual(phases, first) {
		t.Fatal(phases, warnings)
	}
	latest := []Phase{{"P", []Task{{"new", "in_progress"}}}}
	branch := []map[string]any{custom(legacy), toolSnapshot("todowrite", map[string]any{"phases": latest}), custom(StateEntry{"v2", []Phase{}})}
	phases, warnings = Restore(branch)
	if len(warnings) > 0 || len(phases) != 0 {
		t.Fatal(phases, warnings)
	}
	phases, _ = Restore(branch[:2])
	if !reflect.DeepEqual(phases, latest) {
		t.Fatal(phases)
	}
	phases[0].Tasks[0].Content = "changed"
	again, _ := Restore(branch[:2])
	if again[0].Tasks[0].Content != "new" {
		t.Fatal("restore alias")
	}
}

func TestMalformedSnapshotsWarnAndRetainLatestValid(t *testing.T) {
	valid := StateEntry{"v2", []Phase{{"P", []Task{{"keep", "pending"}}}}}
	for _, payload := range []any{map[string]any{}, map[string]any{"schema": "v2", "todos": []any{}}, map[string]any{"phases": nil}, map[string]any{"phases": []any{map[string]any{"tasks": []any{}}}}, map[string]any{"phases": []Phase{{"P", []Task{{"bad", "unknown"}}}}}, map[string]any{"todos": []any{map[string]any{"status": "pending"}}}} {
		phases, warnings := Restore([]map[string]any{custom(valid), custom(payload)})
		if len(warnings) != 1 || !reflect.DeepEqual(phases, valid.Phases) {
			t.Fatal(phases, warnings)
		}
	}
	phases, warnings := Restore(nil)
	if len(phases) != 0 || len(warnings) != 0 {
		t.Fatal(phases, warnings)
	}
}

func TestPersistenceBeforeMemoryAndReadOnlyCalls(t *testing.T) {
	current, _ := ApplyOperation(nil, Operation{Op: "init", Items: []string{"Keep"}})
	original := ClonePhases(current)
	failure := errors.New("disk full")
	calls := 0
	persist := func(entry StateEntry) error {
		calls++
		if entry.Schema != "v2" {
			t.Fatal(entry)
		}
		entry.Phases[0].Tasks[0].Content = "mutated writer copy"
		return failure
	}
	_, _, err := CommitOperation(current, Operation{Op: "done", Task: "Keep"}, persist)
	if !errors.Is(err, failure) || calls != 1 || !reflect.DeepEqual(current, original) {
		t.Fatal(err, calls, current)
	}
	for _, op := range []Operation{{Op: "view"}, {Op: "done", Task: "missing"}} {
		if _, _, err = CommitOperation(current, op, persist); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("read-only persisted")
	}
	details, errs, err := CommitOperation(current, Operation{Op: "done", Task: "Keep"}, func(entry StateEntry) error { calls++; return nil })
	if err != nil || len(errs) > 0 || details.Phases[0].Tasks[0].Status != "completed" || len(details.CompletedTasks) != 1 {
		t.Fatal(details, errs, err)
	}
	if !reflect.DeepEqual(current, original) {
		t.Fatal("caller state changed before publishing")
	}
}
