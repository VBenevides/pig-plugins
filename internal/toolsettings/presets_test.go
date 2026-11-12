package toolsettings

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPresetsAreStagedAndEditable(t *testing.T) {
	m := &Menu{Tabs: [2][]Row{
		{{ID: "read", Enabled: true, DefaultEnabled: true}, {ID: "edit", Enabled: true, DefaultEnabled: true}, {ID: "write", Enabled: true, DefaultEnabled: true}, {ID: "bash", Enabled: true, DefaultEnabled: true}, {ID: "extra", DefaultEnabled: false}},
		{{ID: "skill-a", Enabled: true}, {ID: "skill-b", Enabled: true}},
	}}
	if m.presetLabel() != DefaultPreset {
		t.Fatal(m.presetLabel())
	}
	m.Handle("p")
	if !m.PresetPending || !strings.Contains(strings.Join(m.Render(96), "\n"), "2 Minimum") {
		t.Fatal("preset picker missing")
	}
	if done, save := m.Handle("2"); done || save {
		t.Fatal("preset saved without Enter")
	}
	if m.presetLabel() != MinimumPreset || !m.RestrictTools || !m.RestrictSkills {
		t.Fatal(m.presetLabel())
	}
	for _, r := range m.Tabs[0] {
		if r.Enabled != minimumTool(r.ID) {
			t.Fatalf("tool=%+v", r)
		}
	}
	for _, r := range m.Tabs[1] {
		if r.Enabled {
			t.Fatal("skill enabled")
		}
	}
	// Editing a preset remains possible without opening future tools automatically.
	m.Cursor = 3
	m.Handle(" ")
	if m.presetLabel() != "Custom" || !m.RestrictTools {
		t.Fatal("custom override lost restrictive policy")
	}
	m.Handle("P")
	m.Handle("1")
	if m.presetLabel() != DefaultPreset || m.RestrictTools || m.RestrictSkills {
		t.Fatal("default preset did not restore defaults")
	}
	if m.Tabs[0][4].Enabled {
		t.Fatal("default preset enabled a non-default tool")
	}
	m.Handle("P")
	m.Handle("escape")
	if m.PresetPending {
		t.Fatal("picker did not cancel")
	}
	if done, save := m.Handle("escape"); !done || save {
		t.Fatal("dialog cancel saved choices")
	}
}

func TestMinimumPolicyPersistsAndBlocksFutureEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.ToolAllowlist = []string{"read", "bash", "edit", "write"}
	c.SkillAllowlist = []string{}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.SkillAllowlist == nil {
		t.Fatal("empty allowlist became unrestricted")
	}
	for _, name := range []string{"read", "bash", "edit", "write"} {
		if !c.ToolEnabled(name) {
			t.Fatalf("minimum tool %s disabled", name)
		}
	}
	for _, name := range []string{"browser_navigate", "new_tool"} {
		if c.ToolEnabled(name) {
			t.Fatalf("non-minimum tool %s enabled", name)
		}
	}
	if c.SkillEnabled("/new/skill/SKILL.md") {
		t.Fatal("new skill bypassed preset")
	}
	if paths, err := FilterSkills([]string{"/nonexistent/future/skills"}, c); err != nil || len(paths) != 0 {
		t.Fatalf("no-skills policy accessed roots: %v %v", paths, err)
	}
	c.Reset()
	if c.ToolAllowlist != nil || c.SkillAllowlist != nil || !c.ToolEnabled("new_tool") || !c.SkillEnabled("/new/skill/SKILL.md") {
		t.Fatal("reset retained restrictive preset")
	}
}
