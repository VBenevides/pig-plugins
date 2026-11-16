package toolsearch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestUnloadedToolCallGetsSearchHint(t *testing.T) {
	if !pigtest.UseFusedBinary(t) {
		t.Skip("needs PIG_FUSED_TEST_BINARY")
	}
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})), pigtest.Text("done"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Prompt: "show tasks"})
	body, _ := json.Marshal(mock.Requests()[1])
	if !strings.Contains(string(body), "Call tool_search with a query for it") {
		t.Fatalf("hint missing: %s", body[len(body)-500:])
	}
}
