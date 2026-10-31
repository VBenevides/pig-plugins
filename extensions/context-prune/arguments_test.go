package contextprune

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestProviderBoundsOldWritePayloadsAndKeepsSavedHistory(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	replies := []pigtest.Reply{}
	for index := range 36 {
		replies = append(replies, pigtest.Calls(pigtest.Call("write", map[string]any{"path": fmt.Sprintf("out%d.txt", index), "content": strings.Repeat(fmt.Sprintf("payload-%d ", index), 500)})))
	}
	replies = append(replies, pigtest.Text("done"))
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"write many files"}})
	requests := mock.Requests()
	if len(requests) != 37 {
		t.Fatalf("wrong request count: %d notices=%q stderr=%s", len(requests), result.Notices(), result.Stderr)
	}
	latest := fmt.Sprint(requests[36]["messages"])
	if !strings.Contains(latest, `argument`) || !strings.Contains(latest, "status unchanged; recovery: cr1_") || !strings.Contains(latest, "out35.txt") {
		t.Fatalf("old payloads were not reduced or paths were lost: %s", result.Notices())
	}
	if strings.Count(latest, "payload-35 ") == 0 {
		t.Fatal("newest payload was reduced")
	}
	data, err := os.ReadFile(filepath.Join(home.Work, "out0.txt"))
	if err != nil || !strings.HasPrefix(string(data), "payload-0 ") {
		t.Fatalf("written file changed: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(home.AgentDir(), "sessions", "*", "*.jsonl"))
	saved, err := os.ReadFile(files[0])
	if err != nil || strings.Count(string(saved), "payload-0 payload-0") == 0 {
		t.Fatal("saved session lost the original payload")
	}
}
