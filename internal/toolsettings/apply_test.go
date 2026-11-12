package toolsettings

import (
	"reflect"
	"testing"
)

func TestUnchangedRestrictedSavePreservesOmittedEntries(t *testing.T) {
	cfg := Config{DisabledTools: map[string]bool{}, EnabledTools: map[string]bool{}, DisabledSkills: map[string]bool{}, ToolAllowlist: []string{"visible", "backend"}, SkillAllowlist: []string{"user-only"}}
	m := &Menu{RestrictTools: true, RestrictSkills: true, Tabs: [2][]Row{{{ID: "visible", Enabled: true}}, nil}}
	m.MarkInitial()
	m.ApplyTo(&cfg)
	if !reflect.DeepEqual(cfg.ToolAllowlist, []string{"visible", "backend"}) || !reflect.DeepEqual(cfg.SkillAllowlist, []string{"user-only"}) {
		t.Fatalf("omitted permissions changed: %+v", cfg)
	}
	if len(cfg.DisabledTools) != 0 || len(cfg.EnabledTools) != 0 {
		t.Fatalf("untouched permissions were rewritten: %+v", cfg)
	}
}

func TestExplicitPresetWritesUnchangedCheckbox(t *testing.T) {
	cfg := Config{DisabledTools: map[string]bool{}, EnabledTools: map[string]bool{}, DisabledSkills: map[string]bool{}, ToolAllowlist: []string{"omitted"}}
	m := &Menu{Tabs: [2][]Row{{{ID: "read", Enabled: true, DefaultEnabled: true}, {ID: "extra"}}, nil}}
	m.MarkInitial()
	m.ApplyPreset(MinimumPreset)
	m.ApplyTo(&cfg)
	if !cfg.DisabledTools["extra"] || !cfg.EnabledTools["read"] || !reflect.DeepEqual(cfg.ToolAllowlist, []string{"read"}) {
		t.Fatalf("preset did not apply: %+v", cfg)
	}
}
