package contextprune

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestProviderBoundsOldShellFailuresAndKeepsTheSavedFailure(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "tiny.txt"), []byte("successful neighbor\n"), 0600); err != nil {
		t.Fatal(err)
	}
	replies := []pigtest.Reply{pigtest.Calls(pigtest.Call("bash", map[string]any{"command": "printf '%0.sUNRESOLVED_COMPILER_DIAGNOSTIC ' {1..5000}; exit 7"}))}
	for range 15 {
		replies = append(replies, pigtest.Calls(pigtest.Call("read", map[string]any{"path": "tiny.txt"})))
	}
	replies = append(replies, pigtest.Text("done"))
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"one long task"}})
	requests := mock.Requests()
	if len(requests) != 17 {
		t.Fatalf("wrong request count: %d notices=%q stderr=%s", len(requests), result.Notices(), result.Stderr)
	}
	latest := fmt.Sprint(requests[16]["messages"])
	if !strings.Contains(latest, `historical "bash" failure; status unchanged; no resolution inferred`) || !strings.Contains(latest, "use context_recover") || !strings.Contains(latest, "UNRESOLVED_COMPILER_DIAGNOSTIC") {
		t.Fatalf("failure evidence missing; notices=%q latest=%s", result.Notices(), latest)
	}
	if len(latest) >= len(fmt.Sprint(requests[1]["messages"])) {
		t.Fatal("old failure body was not reduced")
	}
	files, err := filepath.Glob(filepath.Join(home.AgentDir(), "sessions", "*", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing session: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		message, _ := entry["message"].(map[string]any)
		if message["role"] != "toolResult" || message["toolName"] != "bash" {
			continue
		}
		if message["isError"] != true || len(fmt.Sprint(message["content"])) <= 32000 || strings.Contains(fmt.Sprint(message["content"]), "old failure body omitted") {
			t.Fatalf("original failure changed: %v", message["isError"])
		}
		failures++
	}
	if failures != 1 {
		t.Fatalf("original shell command ran %d times", failures)
	}
}
