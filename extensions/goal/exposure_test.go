package goalext

import (
	"path/filepath"
	"slices"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
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

func TestGoalPermissionDenialSurvivesActivation(t *testing.T) {
	_, mock := run(t, []pigtest.Reply{pigtest.Text("waiting"), pigtest.Text("waiting"), pigtest.Text("waiting"), pigtest.Text("waiting")}, func(home *pigtest.Home) {
		if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), toolsettings.Config{DisabledTools: map[string]bool{"goal_finish": true}}); err != nil {
			t.Fatal(err)
		}
	}, "/goal set denied finish")
	if slices.Contains(pigtest.ToolNames(mock), "goal_finish") {
		t.Fatal("goal activation bypassed a global denial")
	}
}

func TestStaleFinishCallRejectsMissingGoal(t *testing.T) {
	x := &extension{}
	_, err := x.finishTool(sdk.Context{}, map[string]any{"outcome": "reached", "summary": "stale"})
	if err == nil || err.Error() != "goal_finish: no goal is active" {
		t.Fatalf("stale call error = %v", err)
	}
}
