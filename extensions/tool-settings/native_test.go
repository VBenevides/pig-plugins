package toolsettingsext

import (
	"path/filepath"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestNativeProfileUsesExclusiveNativeSchemas(t *testing.T) {
	fused := pigtest.UseFusedBinary(t)
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	cfg, err := toolsettings.Load(filepath.Join(home.AgentDir(), toolsettings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg.NativeFiles = true
	cfg.ToolAllowlist = []string{"read", "bash", "edit", "write"}
	cfg.SkillAllowlist = []string{}
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), cfg); err != nil {
		t.Fatal(err)
	}
	settings, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	hashline, err := filepath.Abs("../hashline-edit")
	if err != nil {
		t.Fatal(err)
	}
	extensions := []string{hashline, settings}
	if fused {
		extensions = nil
	}
	mock := pigtest.NewMockLLM(pigtest.Text("done"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensions, Prompts: []string{"hi"}})
	requests := mock.Requests()
	if len(requests) != 1 {
		t.Fatal(len(requests))
	}
	tools, _ := requests[0]["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		function, ok := tool["function"].(map[string]any)
		if !ok {
			function = tool
		}
		name, _ := function["name"].(string)
		if names[name] {
			t.Fatalf("duplicate tool %s", name)
		}
		names[name] = true
		if name == "edit" {
			parameters, _ := function["parameters"].(map[string]any)
			props, _ := parameters["properties"].(map[string]any)
			edits, _ := props["edits"].(map[string]any)
			items, _ := edits["items"].(map[string]any)
			inner, _ := items["properties"].(map[string]any)
			// Native Pi versions support either one replacement or a batch of exact-text replacements.
			exactText := props["oldText"] != nil && props["newText"] != nil || inner["oldText"] != nil && inner["newText"] != nil
			if !exactText || props["version"] != nil || inner["anchor"] != nil {
				t.Fatalf("not native edit: %v", props)
			}
		}
	}
	if len(names) != 4 || !names["read"] || !names["bash"] || !names["edit"] || !names["write"] {
		t.Fatal(names)
	}
}
