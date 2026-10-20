package goalext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/goal"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// This exercises the bundled command, not a separately loaded source extension.
func TestFusedGoalResume(t *testing.T) {
	binary := os.Getenv("PIG_FUSED_TEST_BINARY")
	if binary == "" {
		t.Skip("set PIG_FUSED_TEST_BINARY to a candidate fused binary")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "pig")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"settings.json":     `{"retry":{"enabled":false},"compaction":{"enabled":false}}`,
		"auto-model.json":   `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`,
		"check-update.json": `{"enabled":false}`,
	} {
		if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mock := pigtest.NewMockLLM(pigtest.HTTPError(400, "fixture stopped"), pigtest.Text("checkpoint"), pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "reached", "summary": "resumed existing work"})), pigtest.Text("done"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Prompts: []string{"/goal resume", "/goal set finish existing work", "checkpoint", "/goal resume"}})
	notices := strings.Join(result.Notices(), "\n")
	for _, want := range []string{"No unfinished goal to resume", "Goal resumed.", "resumed existing work"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("missing %q in %s; stderr=%s", want, notices, result.Stderr)
		}
	}
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests=%d, want 4; notices=%s", len(requests), notices)
	}
	if !strings.Contains(systemText(requests[2]), "Objective: finish existing work") || lastUserText(requests[2]) != goal.ContinueMessage {
		t.Fatalf("resume lost objective or continuation: %v", requests[2])
	}
}
