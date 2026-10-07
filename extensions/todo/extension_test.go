package todoext

import (
	"encoding/json"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func finished(t testing.TB, events []map[string]any) []tasks.Details {
	t.Helper()
	var out []tasks.Details
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
	first := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "add", "text": "first"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "add", "text": "second"})), pigtest.Text("done"))
	defer first.Close()
	result := home.Run(t, first, pigtest.RunOptions{Extensions: []string{ext}})
	if result.ExitCode != 0 {
		t.Fatalf("initial: %s %s", result.Stdout, result.Stderr)
	}
	second := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "list"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "toggle", "id": 1})), pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "clear"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "add", "text": "reset"})), pigtest.Text("done"))
	defer second.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: second, Models: []string{"mock-model"}}})
	result = home.RunPig(t, pigtest.RunOptions{Extensions: []string{ext}}, "-p", "--mode", "json", "--continue", "--provider", "mock", "--model", "mock-model")
	if result.ExitCode != 0 {
		t.Fatalf("resume: %s %s", result.Stdout, result.Stderr)
	}
	details := finished(t, jsonEvents(t, result.Stdout))
	want := []tasks.Details{
		{Action: "list", Todos: []tasks.Item{{ID: 1, Text: "first"}, {ID: 2, Text: "second"}}, NextID: 3},
		{Action: "toggle", Todos: []tasks.Item{{ID: 1, Text: "first", Done: true}, {ID: 2, Text: "second"}}, NextID: 3},
		{Action: "clear", Todos: []tasks.Item{}, NextID: 1},
		{Action: "add", Todos: []tasks.Item{{ID: 1, Text: "reset"}}, NextID: 2},
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
		pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "add", "text": "first"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "add", "text": "second"})), pigtest.Text("done"),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "list"})), pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "add", "text": "branch"})), pigtest.Text("done"),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"action": "list"})), pigtest.Text("done"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext, nav}, Prompts: []string{"Add initial tasks", "/todo-test-branch", "List and add a branch task", "/todo-test-new", "List new session tasks"}})
	details := finished(t, result.Events)
	want := []tasks.Details{
		{Action: "add", Todos: []tasks.Item{{ID: 1, Text: "first"}}, NextID: 2},
		{Action: "add", Todos: []tasks.Item{{ID: 1, Text: "first"}, {ID: 2, Text: "second"}}, NextID: 3},
		{Action: "list", Todos: []tasks.Item{{ID: 1, Text: "first"}}, NextID: 2},
		{Action: "add", Todos: []tasks.Item{{ID: 1, Text: "first"}, {ID: 2, Text: "branch"}}, NextID: 3},
		{Action: "list", Todos: []tasks.Item{}, NextID: 1},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("branch/replacement %#v want %#v; events=%v stderr=%s", details, want, result.Events, result.Stderr)
	}
}
