package toolsettings

import (
	"strings"
	"testing"
)

func TestBenchmarkPresetDisablesQuestionsAndKeepsDefaults(t *testing.T) {
	m := &Menu{NativeFiles: true, RestrictTools: true, RestrictSkills: true, Tabs: [2][]Row{{
		{ID: "read", Variant: "hashline", DefaultEnabled: true},
		{ID: "read", Variant: "native", Enabled: true, DefaultEnabled: true},
		{ID: "bash", DefaultEnabled: true},
		{ID: "ask_user_question", Enabled: true, DefaultEnabled: true},
		{ID: "optional", DefaultEnabled: false},
	}, {{ID: "skill"}}}}
	m.MarkInitial()
	m.Handle("p")
	if !strings.Contains(strings.Join(m.Render(96), "\n"), "4 default-benchmark") {
		t.Fatal("benchmark profile missing from chooser")
	}
	m.Handle("4")
	if m.PresetPending || !m.NativeFiles || m.RestrictTools || m.RestrictSkills || m.presetLabel() != BenchmarkPreset {
		t.Fatalf("profile state: %+v", m)
	}
	for i, want := range []bool{false, true, true, false, false} {
		if m.Tabs[0][i].Enabled != want {
			t.Fatalf("tool %d enabled=%v want=%v", i, m.Tabs[0][i].Enabled, want)
		}
	}
	if !m.Tabs[1][0].Enabled {
		t.Fatal("benchmark disabled skills")
	}
	cfg := Config{DisabledTools: map[string]bool{}, EnabledTools: map[string]bool{}, DisabledSkills: map[string]bool{}}
	m.ApplyTo(&cfg)
	if cfg.ToolEnabled("ask_user_question") || !cfg.ToolEnabled("bash") || cfg.ToolAllowlist != nil || cfg.SkillAllowlist != nil {
		t.Fatalf("saved benchmark permissions: %+v", cfg)
	}
	m.ApplyPreset(DefaultPreset)
	m.ApplyTo(&cfg)
	if !cfg.ToolEnabled("ask_user_question") || m.presetLabel() != DefaultPreset {
		t.Fatal("Default did not restore questions")
	}
}
