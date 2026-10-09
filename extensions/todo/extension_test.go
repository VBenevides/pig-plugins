package todoext

import (
	"encoding/json"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func finished(t testing.TB, events []map[string]any) []tasks.PhasedDetails {
	t.Helper()
	var out []tasks.PhasedDetails
	for _, event := range events {
		if event["type"] != "tool_execution_end" || event["toolName"] != "todo" {
			continue
		}
		result, ok := event["result"].(map[string]any)
		if !ok {
			t.Fatalf("todo result has no result object: %#v", event)
		}
		d, err := tasks.Decode(result["details"])
		if err != nil {
			t.Fatalf("invalid todo result details: %v; event=%#v", err, event)
		}
		out = append(out, d)
	}
	return out
}
func jsonEvents(t testing.TB, text string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.Lines(text) {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid PiG JSON event: %v; line=%q", err, line)
		}
		out = append(out, event)
	}
	return out
}
func TestPiGResume(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	first := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "init", "items": []string{"first"}})), pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "append", "phase": "Tasks", "items": []string{"second"}})), pigtest.Text("done"))
	defer first.Close()
	result := home.Run(t, first, pigtest.RunOptions{Extensions: []string{ext}})
	if result.ExitCode != 0 {
		t.Fatalf("initial: %s %s", result.Stdout, result.Stderr)
	}
	second := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "done", "task": "first"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "rm"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "init", "items": []string{"reset"}})), pigtest.Text("done"))
	defer second.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: second, Models: []string{"mock-model"}}})
	result = home.RunPig(t, pigtest.RunOptions{Extensions: []string{ext}}, "-p", "--mode", "json", "--continue", "--provider", "mock", "--model", "mock-model")
	if result.ExitCode != 0 {
		t.Fatalf("resume: %s %s", result.Stdout, result.Stderr)
	}
	details := finished(t, jsonEvents(t, result.Stdout))
	want := []tasks.PhasedDetails{
		{Op: "view", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "first", Status: "in_progress"}, {Content: "second", Status: "pending"}}}}, Storage: "session"},
		{Op: "done", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "first", Status: "completed"}, {Content: "second", Status: "in_progress"}}}}, Storage: "session", CompletedTasks: []tasks.Completion{{Phase: "Tasks", Content: "first"}}},
		{Op: "rm", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{}}}, Storage: "session"},
		{Op: "init", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "reset", Status: "in_progress"}}}}, Storage: "session"},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("resumed details %#v want %#v; stdout=%s stderr=%s", details, want, result.Stdout, result.Stderr)
	}
}
func TestPiGBranchAndSessionReplacement(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	nav, err := filepath.Abs("../../testfixtures/session-navigation")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "init", "items": []string{"first"}})), pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "append", "phase": "Tasks", "items": []string{"second"}})), pigtest.Text("done"),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "append", "phase": "Tasks", "items": []string{"branch"}})), pigtest.Text("done"),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})), pigtest.Text("done"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext, nav}, Prompts: []string{"Add initial tasks", "/todo-test-branch", "List and add a branch task", "/todo-test-new", "List new session tasks"}})
	details := finished(t, result.Events)
	want := []tasks.PhasedDetails{
		{Op: "init", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "first", Status: "in_progress"}}}}, Storage: "session"},
		{Op: "append", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "first", Status: "in_progress"}, {Content: "second", Status: "pending"}}}}, Storage: "session"},
		{Op: "view", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "first", Status: "in_progress"}}}}, Storage: "session"},
		{Op: "append", Phases: []tasks.Phase{{Name: "Tasks", Tasks: []tasks.Task{{Content: "first", Status: "in_progress"}, {Content: "branch", Status: "pending"}}}}, Storage: "session"},
		{Op: "view", Phases: []tasks.Phase{}, Storage: "session"},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("branch/replacement %#v want %#v; events=%v stderr=%s", details, want, result.Events, result.Stderr)
	}
}

func TestPiGOnlySuccessfulMutationsPersist(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "init", "items": []string{"keep"}})),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "done", "task": "missing"})),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "start", "task": "keep"})),
		pigtest.Text("done"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}})
	if result.ExitCode != 0 {
		t.Fatal(result)
	}
	count := 0
	err = filepath.WalkDir(home.AgentDir(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, event := range jsonEvents(t, string(data)) {
			if event["customType"] == tasks.StateEntryType {
				count++
				payload, ok := event["data"].(map[string]any)
				if !ok || payload["schema"] != "v2" {
					t.Fatalf("bad durable payload: %v", event)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("persisted %d state entries, want 2", count)
	}
}
