package goalext

import (
	"slices"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestNoGoalOmitsFinishDeclaration(t *testing.T) {
	_, mock := run(t, []pigtest.Reply{pigtest.Text("Ready.")}, nil, "hello")
	if slices.Contains(pigtest.ToolNames(mock), "goal_finish") {
		t.Fatal("goal_finish is declared without an active goal")
	}
}

func TestActiveGoalDeclaresFinish(t *testing.T) {
	_, mock := run(t, []pigtest.Reply{
		pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "reached", "summary": "done"})),
		pigtest.Text("Done."),
	}, nil, "/goal set finish this task")
	if !slices.Contains(pigtest.ToolNames(mock), "goal_finish") {
		t.Fatal("goal_finish is absent during an active goal")
	}
	requests := mock.Requests()
	if len(requests) < 2 {
		t.Fatal("missing request after goal completion")
	}
	tools, _ := requests[1]["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		if fn["name"] == "goal_finish" {
			t.Fatal("goal_finish remains declared after completion")
		}
	}
}
