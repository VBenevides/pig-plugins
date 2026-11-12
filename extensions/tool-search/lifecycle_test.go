package toolsearch

import (
	"path/filepath"
	"slices"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func requestNames(request map[string]any) []string {
	var names []string
	tools, _ := request["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names = append(names, name)
	}
	return names
}

func TestDiscoveryExpiresAfterRequest(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	search, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	todo, err := filepath.Abs("../todo")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "todo", "limit": 1})),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})),
		pigtest.Text("done"), pigtest.Text("new request"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{search, todo}, Prompts: []string{"inspect tasks", "hello again"}})
	for _, event := range result.Events {
		if event["type"] == "extension_error" {
			t.Fatalf("extension error: %v", event)
		}
	}
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests=%d", len(requests))
	}
	for i, want := range []bool{false, true, true, false} {
		if got := slices.Contains(requestNames(requests[i]), "todo"); got != want {
			t.Fatalf("request %d todo visible=%v want=%v", i, got, want)
		}
	}
}

func TestDiscoveryDoesNotExposeDeniedSchema(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), toolsettings.Config{DisabledTools: map[string]bool{"todo": true}}); err != nil {
		t.Fatal(err)
	}
	search, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	todo, err := filepath.Abs("../todo")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "todo", "limit": 1})), pigtest.Text("done"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{search, todo}, Prompt: "find tasks"})
	requests := mock.Requests()
	if len(requests) != 2 || slices.Contains(requestNames(requests[1]), "todo") {
		t.Fatal("denied schema was exposed")
	}
}

func TestSearchBounds(t *testing.T) {
	result, err := boundSearch(sdk.Context{}, map[string]any{"toolName": "tool_search", "input": map[string]any{"limit": float64(21)}})
	if err != nil || result == nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
}
