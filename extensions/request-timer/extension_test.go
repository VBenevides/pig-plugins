package requesttimerext

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestTimerSpansToolFollowUpAndClearsAtEnd(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": "missing-file"})}, Delay: 1200 * time.Millisecond},
		pigtest.Reply{Text: "done", Delay: 1200 * time.Millisecond},
		pigtest.Reply{Text: "next", Delay: 1200 * time.Millisecond},
	)
	defer mock.Close()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{path}, Prompts: []string{"test timer", "next request"}})
	statuses := result.Statuses(Name)
	starts := 0
	for _, s := range statuses {
		if s == "Working - 00h 00m 00s" {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("request resets: %v", statuses)
	}
	if !slices.Contains(statuses, "Working - 00h 00m 02s") {
		t.Fatalf("timer did not span tool follow-up: %v", statuses)
	}
	if len(statuses) == 0 || statuses[len(statuses)-1] != "" {
		t.Fatalf("timer not cleared: %v", statuses)
	}
}
