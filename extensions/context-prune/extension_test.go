package contextprune

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/contextprune"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func extensionDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// runTwoTasks reads one large file twice and two small files in the first task, then runs a second task. It
// returns the request bodies, one string each.
func runTwoTasks(t *testing.T, prompts ...string) ([]string, pigtest.RPCResult) {
	t.Helper()
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	for name, text := range map[string]string{"big.txt": strings.Repeat("needle-line\n", 3000), "a.txt": "a\n", "b.txt": "b\n"} {
		if err := os.WriteFile(filepath.Join(home.Work, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) pigtest.Reply {
		return pigtest.Calls(pigtest.Call("read", map[string]any{"path": path}))
	}
	mock := pigtest.NewMockLLM(read("big.txt"), read("big.txt"), read("a.txt"), read("b.txt"), pigtest.Text("first done"), pigtest.Text("second done"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: prompts})
	for _, event := range result.Events {
		if event["type"] == "extension_error" {
			t.Fatalf("extension failed: %v", event)
		}
	}
	var bodies []string
	for _, request := range mock.Requests() {
		data, err := json.Marshal(request["messages"])
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(data))
	}
	return bodies, result
}

func TestPruningIsOffByDefault(t *testing.T) {
	bodies, _ := runTwoTasks(t, "task one", "task two")
	if len(bodies) != 6 {
		t.Fatalf("want 6 model requests, got %d", len(bodies))
	}
	for i, body := range bodies {
		if strings.Contains(body, "[pruned:") {
			t.Fatalf("request %d was pruned while the setting is off", i)
		}
	}
}

func TestPrunesStaleReadsFromTheNextRunOnAndKeepsTheHistory(t *testing.T) {
	bodies, result := runTwoTasks(t, "/context-prune on", "task one", "task two")
	if len(bodies) != 6 {
		t.Fatalf("want 6 model requests, got %d; notices %q; stderr %s", len(bodies), result.Notices(), result.Stderr)
	}
	for i := 0; i < 5; i++ {
		if strings.Contains(bodies[i], "[pruned:") {
			t.Fatalf("request %d is inside the first run, whose prefix must not change", i)
		}
	}
	last := bodies[5]
	if strings.Count(last, "[pruned: this read of big.txt is out of date") != 1 {
		t.Fatalf("the older read of big.txt must be replaced once in the next run")
	}
	if strings.Count(last, "needle-line") == 0 || strings.Count(last, "needle-line") >= strings.Count(bodies[4], "needle-line") {
		t.Fatalf("the newest read stays and the older copy leaves: %d vs %d", strings.Count(last, "needle-line"), strings.Count(bodies[4], "needle-line"))
	}
	if !strings.Contains(strings.Join(result.Notices(), "\n"), "context-prune: on") {
		t.Fatalf("missing confirmation: %q", result.Notices())
	}
}

func TestCommandValidatesPersistsAndReports(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{
		"/context-prune", "/context-prune bogus", "/context-prune on", "/context-prune status", "/context-prune off",
	}})
	notices := result.Notices()
	if len(notices) != 5 {
		t.Fatalf("notices = %q; stderr %s", notices, result.Stderr)
	}
	for i, want := range []string{"context-prune: off", "usage:", "context-prune: on", "context-prune: on", "context-prune: off"} {
		if !strings.Contains(notices[i], want) {
			t.Errorf("notice %d lacks %q: %s", i, want, notices[i])
		}
	}
	enabled, err := contextprune.Enabled(filepath.Join(home.AgentDir(), contextprune.FileName))
	if err != nil || enabled {
		t.Fatalf("file must hold off after the last command: %v %v", enabled, err)
	}
}

func TestDamagedSettingsFileIsReportedAndPrunesNothing(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), contextprune.FileName), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("hello back"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"hello"}})
	if !strings.Contains(strings.Join(result.Notices(), "\n"), "pruning is off") {
		t.Fatalf("a damaged file must be reported: %q; stderr %s", result.Notices(), result.Stderr)
	}
}
