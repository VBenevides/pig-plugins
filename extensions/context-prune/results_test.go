package contextprune

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestProviderBoundsAccumulatedSmallShellResults(t *testing.T) {
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
		command := fmt.Sprintf("head -c 3000 /dev/zero | tr '\\0' 'a'; echo marker-%d", index)
		replies = append(replies, pigtest.Calls(pigtest.Call("bash", map[string]any{"command": command})))
	}
	replies = append(replies, pigtest.Text("done"))
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"collect output"}})
	requests := mock.Requests()
	if len(requests) != 37 {
		t.Fatalf("wrong request count: %d notices=%q stderr=%s", len(requests), result.Notices(), result.Stderr)
	}
	latest := fmt.Sprint(requests[36]["messages"])
	if !strings.Contains(latest, "status unchanged; recovery: cr1_") || !strings.Contains(latest, "marker-35") {
		t.Fatalf("old results were not bounded or newest was changed: %s", result.Notices())
	}
	if len(latest) >= len(fmt.Sprint(requests[35]["messages"]))+4000 {
		t.Fatal("request kept growing at full result size")
	}
}
