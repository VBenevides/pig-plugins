package browseruseext

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func extensionPaths(t *testing.T) []string {
	t.Helper()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return []string{path}
}

func TestModeVisibilityAndInvalidArguments(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("off"), pigtest.Text("chrome"), pigtest.Text("firefox"), pigtest.Text("off again"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensionPaths(t), Prompts: []string{
		"check disabled", "/browser-use mode chrome", "check chrome", "/browser-use mode firefox", "/browser-use mode invalid", "check firefox", "/browser-use mode off", "check disabled again",
	}})
	want := []string{"Browser use: chrome", "Browser use: firefox", "usage: /browser-use mode off|chrome|firefox", "Browser use: off"}
	if !slices.Equal(result.Notices(), want) {
		t.Fatalf("notices %q; stderr: %s", result.Notices(), result.Stderr)
	}
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests = %d", len(requests))
	}
	for i, req := range requests {
		enabled := i == 1 || i == 2
		raw, _ := json.Marshal(req["tools"])
		for _, name := range toolNames() {
			if strings.Contains(string(raw), name) != enabled {
				t.Errorf("request %d: visibility for %s: %s", i, name, raw)
			}
		}
		messages, _ := req["messages"].([]any)
		for _, message := range messages {
			m, _ := message.(map[string]any)
			if m["role"] != "system" {
				continue
			}
			text, _ := json.Marshal(m["content"])
			if strings.Contains(string(text), "browser_snapshot") != enabled {
				t.Errorf("request %d: system prompt visibility: %s", i, text)
			}
		}
	}
	statuses := result.Statuses(Name)
	if !slices.Equal(statuses, []string{"", "Browser: chrome", "Browser: firefox", ""}) {
		t.Fatalf("statuses: %q", statuses)
	}
	for _, event := range result.Events {
		if event["type"] == "extension_error" {
			t.Fatalf("extension failed: %v", event)
		}
	}
}

func TestDisabledToolsCannotBeDiscovered(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "browser"})), pigtest.Text("done"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensionPaths(t), Prompts: []string{"find browser tools"}})
	requests := mock.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests %d", len(requests))
	}
	raw, _ := json.Marshal(requests[1]["messages"])
	for _, name := range toolNames() {
		if strings.Contains(string(raw), name) {
			t.Errorf("disabled tool discovered: %s", raw)
		}
	}
}

func TestDisabledExecuteAndCompactRenderer(t *testing.T) {
	s := &state{}
	value, err := s.execute(sdk.Context{}, "snapshot", nil)
	if err != nil || !value.(sdk.ToolResult).IsError {
		t.Fatalf("off execution: %v %v", value, err)
	}
	text := strings.Repeat("line of untrusted page text\n", 20)
	for _, expanded := range []bool{false, true} {
		lines, err := renderResult(sdk.Context{}, sdk.ToolRenderResult{Content: []map[string]any{{"type": "text", "text": text}}}, sdk.ToolRenderResultOptions{Expanded: expanded}, sdk.ToolRenderContext{}, 30)
		if err != nil {
			t.Fatal(err)
		}
		if !expanded && len(lines) > 6 {
			t.Fatal("collapsed result is not bounded")
		}
		if expanded && len(lines) <= 6 {
			t.Fatal("expanded result missing")
		}
	}
}
