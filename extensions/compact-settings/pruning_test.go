package compactsettings

import (
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestUniqueOldReadCanAvoidPressureSummary(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	writeBigRunFiles(t, home)
	replies := []pigtest.Reply{{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": "big.txt"})}, PromptTokens: 10}}
	for turn := 1; turn < 12; turn++ {
		tokens := 10
		if turn == 11 {
			tokens = 60000
		}
		replies = append(replies, pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": "a.txt"})}, PromptTokens: tokens})
	}
	replies = append(replies, pigtest.Text("done"))
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t), pruneExtensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "one long task"},
	})
	requests := mock.Requests()
	if countSummaryRequests(t, mock) != 0 || len(requests) != 13 {
		t.Fatalf("age masking should avoid the summary: requests=%d notices=%q stderr=%s", len(requests), result.Notices(), result.Stderr)
	}
	last := requestText(t, requests[12])
	if !strings.Contains(last, "[pruned: old read of big.txt") || !strings.Contains(last, "[recovery: toolResult") {
		t.Fatal("pressure must mask a unique old read using the durable recovery source")
	}
}
