package toolsettingsext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestMinimumPresetUsesBashAndHashlineEdit(t *testing.T) {
	fused := pigtest.UseFusedBinary(t)
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	skillDir := filepath.Join(home.AgentDir(), "preset-skills", "example")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: example\ndescription: preset skill must not appear\n---\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "pig-plugins.json"), []byte(`{"skillPaths":["preset-skills"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := toolsettings.Load(filepath.Join(home.AgentDir(), toolsettings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	c.ToolAllowlist = []string{"read", "bash", "edit", "write"}
	c.SkillAllowlist = []string{}
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), c); err != nil {
		t.Fatal(err)
	}
	settings, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := filepath.Abs("../user-resources")
	if err != nil {
		t.Fatal(err)
	}
	hashlineExtension, err := filepath.Abs("../hashline-edit")
	if err != nil {
		t.Fatal(err)
	}
	extensions := []string{resources, hashlineExtension, settings}
	if fused {
		extensions = nil
	}
	if err := os.WriteFile(filepath.Join(home.Work, "seed.txt"), []byte("PRESET_READ_OK"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": "seed.txt"}), pigtest.Call("bash", map[string]any{"command": "printf PRESET_BASH_OK"})}}, pigtest.Text("done"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensions, Prompts: []string{"hi"}})
	requests := mock.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	tools, _ := requests[0]["tools"].([]any)
	names := map[string]bool{}
	hashlineEdit := false
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("invalid tool schema: %T", raw)
		}
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		if name == "" {
			name, _ = tool["name"].(string)
			function = tool
		}
		if name == "edit" {
			parameters, _ := function["parameters"].(map[string]any)
			actual, err := json.Marshal(parameters["properties"])
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.Marshal(hashline.EditSchema()["properties"])
			if err != nil {
				t.Fatal(err)
			}
			hashlineEdit = string(actual) == string(expected)
		}
		names[name] = true
	}
	if len(names) != 4 || !names["read"] || !names["bash"] || !names["edit"] || !names["write"] {
		t.Fatalf("preset tool names=%v", names)
	}
	if !hashlineEdit {
		t.Fatal("minimum preset did not expose the hashline edit schema")
	}
	raw, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "preset skill must not appear") {
		t.Fatal("preset leaked skill metadata")
	}
	result, err := json.Marshal(requests[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "PRESET_READ_OK") {
		t.Fatal("allowlisted read did not execute")
	}
	if !strings.Contains(string(result), "PRESET_BASH_OK") {
		t.Fatal("allowlisted bash did not execute")
	}
}
