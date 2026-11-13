package toolsettingsext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestRPCDisabledToolAndSkillStayOutOfRequests(t *testing.T) {
	fused := pigtest.UseFusedBinary(t)
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	skills := filepath.Join(home.AgentDir(), "skills")
	if !fused {
		// Plain pig also discovers its default skills independently. Test extension-owned paths here;
		// the fused build below tests the real default skills folder with ambient discovery disabled.
		skills = filepath.Join(home.AgentDir(), "custom-skills")
		if err := os.MkdirAll(home.AgentDir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home.AgentDir(), "pig-plugins.json"), []byte(`{"skillPaths":["custom-skills"]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"allowed", "disabled"} {
		dir := filepath.Join(skills, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: " + name + " skill discovery\n---\nSECRET_SKILL_BODY_" + name
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := toolsettings.Config{DisabledTools: map[string]bool{"bash": true}, DisabledSkills: map[string]bool{toolsettings.Canonical(filepath.Join(skills, "disabled", "SKILL.md")): true}}
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), cfg); err != nil {
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
	mock := pigtest.NewMockLLM(pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("bash", map[string]any{"command": "touch tool-settings-must-not-exist"})}}, pigtest.Text("done"))
	defer mock.Close()
	extensions := []string{resources, settings}
	if fused {
		extensions = nil
	}
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensions, Prompts: []string{"/skill:disabled"}})
	requests := mock.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	if _, err := os.Stat(filepath.Join(home.Work, "tool-settings-must-not-exist")); !os.IsNotExist(err) {
		t.Fatalf("disabled bash ran: %v", err)
	}
	tools, _ := requests[0]["tools"].([]any)
	for _, v := range tools {
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), `"name":"bash"`) {
			t.Fatalf("disabled bash declaration: %s", raw)
		}
	}
	raw, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "disabled skill discovery") || strings.Contains(text, "SECRET_SKILL_BODY_disabled") {
		t.Fatal("disabled skill supplied to model")
	}
	if !strings.Contains(text, "allowed skill discovery") {
		t.Fatal("enabled neighbor missing")
	}
}
