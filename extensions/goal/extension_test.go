package goalext

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/goal"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func run(t *testing.T, script []pigtest.Reply, setup func(home *pigtest.Home), prompts ...string) (pigtest.RPCResult, *pigtest.MockLLM) {
	t.Helper()
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if setup != nil {
		setup(home)
	}
	mock := pigtest.NewMockLLM(script...)
	t.Cleanup(mock.Close)
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{path}, Prompts: prompts})
	for _, event := range result.Events {
		if event["type"] == "extension_error" || event["notifyType"] == "error" {
			t.Fatalf("extension failed: %v", event)
		}
	}
	return result, mock
}

func lastUserText(request map[string]any) string {
	messages, _ := request["messages"].([]any)
	for _, raw := range slices.Backward(messages) {
		message, _ := raw.(map[string]any)
		if message["role"] != "user" {
			continue
		}
		if text, ok := message["content"].(string); ok {
			return text
		}
		encoded, _ := message["content"].([]any)
		var parts []string
		for _, part := range encoded {
			if block, _ := part.(map[string]any); block["text"] != nil {
				parts = append(parts, block["text"].(string))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func systemText(request map[string]any) string {
	messages, _ := request["messages"].([]any)
	var out []string
	for _, raw := range messages {
		if message, _ := raw.(map[string]any); message["role"] == "system" {
			text, _ := message["content"].(string)
			out = append(out, text)
		}
	}
	return strings.Join(out, "\n")
}

func TestGoalContinuesUntilFinished(t *testing.T) {
	result, mock := run(t, []pigtest.Reply{
		pigtest.Text("I looked around."),
		pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "reached", "summary": "all done"})),
		pigtest.Text("Finished."),
	}, nil, "/goal set make it work")
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3; stderr: %s", len(requests), result.Stderr)
	}
	if got := lastUserText(requests[0]); got != "Goal: make it work" {
		t.Fatalf("first message = %q", got)
	}
	if !strings.Contains(systemText(requests[0]), "ACTIVE GOAL") {
		t.Fatal("system prompt lacks the goal")
	}
	if got := lastUserText(requests[1]); got != goal.ContinueMessage {
		t.Fatalf("continuation = %q", got)
	}
	if got := result.Statuses(Name); !slices.Equal(got, []string{"", "GOAL", ""}) {
		t.Fatalf("badge = %q", got)
	}
	last := result.Notices()[len(result.Notices())-1]
	for _, want := range []string{"Current goal: make it work", "finished (reached)", "Result: all done"} {
		if !strings.Contains(last, want) {
			t.Fatalf("final notice %q lacks %q", last, want)
		}
	}
}

func TestGoalStopsWithoutProgress(t *testing.T) {
	result, mock := run(t, []pigtest.Reply{
		pigtest.Text("one"), pigtest.Text("two"), pigtest.Text("three"), pigtest.Text("never sent"),
	}, nil, "/goal set chat forever")
	if got := len(mock.Requests()); got != maxIdleRuns {
		t.Fatalf("requests = %d, want %d", got, maxIdleRuns)
	}
	last := result.Notices()[len(result.Notices())-1]
	if !strings.Contains(last, "finished (stopped)") || !strings.Contains(last, "no progress") {
		t.Fatalf("final notice = %q", last)
	}
}

func TestGoalBlockedIsReported(t *testing.T) {
	result, mock := run(t, []pigtest.Reply{
		pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "blocked", "summary": "needs a key"})),
		pigtest.Text("Blocked."),
	}, nil, "/goal set deploy")
	if got := len(mock.Requests()); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	last := result.Notices()[len(result.Notices())-1]
	if !strings.Contains(last, "finished (blocked)") || !strings.Contains(last, "Result: needs a key") {
		t.Fatalf("final notice = %q", last)
	}
}

func TestGoalCommandsWithoutGoal(t *testing.T) {
	result, mock := run(t, nil, nil, "/goal check", "/goal clear", "/goal", "/goal set", "/goal bogus")
	want := []string{
		"No goal. Use /goal set <objective>.", "No goal to clear.",
		"usage: /goal set <objective> | /goal resume | /goal check | /goal clear", "usage: /goal set <objective>",
		"usage: /goal set <objective> | /goal resume | /goal check | /goal clear",
	}
	if got := result.Notices(); !slices.Equal(got, want) {
		t.Fatalf("notices = %q, want %q", got, want)
	}
	if len(mock.Requests()) != 0 {
		t.Fatal("a command without an objective must not call the model")
	}
}

func TestGoalCheckWhileWorkingAndClearStopsContinuation(t *testing.T) {
	result, mock := run(t, []pigtest.Reply{
		{Text: "slow", Delay: 600 * time.Millisecond},
		pigtest.Text("must not be requested"),
	}, nil, "/goal set deploy", "/goal check", "/goal clear")
	if got := len(mock.Requests()); got != 1 {
		t.Fatalf("requests = %d, want 1 (clear must stop the loop)", got)
	}
	notices := strings.Join(result.Notices(), "\n")
	for _, want := range []string{"Current goal: deploy\nCurrent status: working", "Goal cleared"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("notices %q lack %q", notices, want)
		}
	}
}

func TestGoalExpandsSkillsInObjective(t *testing.T) {
	_, mock := run(t, []pigtest.Reply{
		pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "reached", "summary": "ok"})),
		pigtest.Text("Done."),
	}, func(home *pigtest.Home) {
		dir := filepath.Join(home.AgentDir(), "skills", "demo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: demo\ndescription: Demo skill\n---\nUse the demo procedure.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}, "/goal set fix it, following /skill:demo, and /skill:unknown stays text")
	first := lastUserText(mock.Requests()[0])
	if !strings.Contains(first, "<skill name=\"demo\"") || !strings.Contains(first, "Use the demo procedure.") {
		t.Fatalf("skill not expanded: %q", first)
	}
	if strings.Contains(first, "name: demo") || strings.Contains(first, "<skill name=\"unknown\"") {
		t.Fatalf("frontmatter leaked or unknown skill expanded: %q", first)
	}
	if !strings.HasSuffix(first, "Goal: fix it, following /skill:demo, and /skill:unknown stays text") {
		t.Fatalf("objective missing: %q", first)
	}
}

func TestGoalFinishWithoutGoalFails(t *testing.T) {
	_, mock := run(t, []pigtest.Reply{
		pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "reached", "summary": "x"})),
		pigtest.Text("ok"),
	}, nil, "plain prompt")
	results := pigtest.ToolResults(mock)
	if len(results) != 1 || !strings.Contains(results[0], "no goal is active") {
		t.Fatalf("tool results = %q", results)
	}
}
