package toolsettings

import (
	"slices"
	"strings"
	"testing"
)

func groupedMenu() *Menu {
	m := &Menu{Tabs: [2][]Row{{
		{Name: "bash", ID: "bash", Group: "Shell", Enabled: true, Visible: true, Context: "1234"},
		{Name: "web_a", ID: "web_a", Group: "Web", Enabled: true, Visible: true, Context: "12345678"},
		{Name: "web_b", ID: "web_b", Group: "Web", Visible: true, Context: "1234"},
	}, nil}}
	m.CollapseGroups("Web")
	return m
}

func TestGroupHeaderToggleAndFold(t *testing.T) {
	m := groupedMenu()
	text := strings.Join(m.Render(96), "\n")
	for _, part := range []string{"> Shell (1/1)", "v Web (1/2)", "[~]", "    web_a", "    web_b"} {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q:\n%s", part, text)
		}
	}
	if strings.Contains(text, "    bash") {
		t.Fatal("collapsed group rows are shown")
	}
	m.Handle("down") // Web header
	m.Handle(" ")    // partial -> all on
	if !m.Tabs[0][1].Enabled || !m.Tabs[0][2].Enabled {
		t.Fatal("header did not enable every tool")
	}
	m.Handle(" ") // all on -> all off
	if m.Tabs[0][1].Enabled || m.Tabs[0][2].Enabled || !m.Tabs[0][0].Enabled {
		t.Fatal("header did not disable only its own tools")
	}
	m.Handle("down") // web_a
	m.Handle(" ")
	if !m.Tabs[0][1].Enabled || m.Tabs[0][2].Enabled {
		t.Fatal("tool toggle affected neighbours")
	}
	m.Handle("left") // collapse and return to header
	if !m.Collapsed["Web"] || m.Cursor != 1 {
		t.Fatalf("collapsed=%v cursor=%d", m.Collapsed, m.Cursor)
	}
	m.Handle("right")
	if m.Collapsed["Web"] {
		t.Fatal("right did not expand")
	}
}

func TestGroupFor(t *testing.T) {
	for _, tt := range []struct{ name, source, want string }{
		{"read", "builtin", "Files"}, {"bash", "", "Shell"}, {"mcp__github__list", "x", "MCP: github"},
		{"memory_read", "/h/ext/pi-curator/index.js", "pi-curator"}, {"foo", "/h/ext/web.js", "web"}, {"x", "", "Core"}, {"web_search", "/h/ext/web-search/index.js", "Web & Search"}, {"mcp__exa__web_search_exa", "x", "Web & Search"}, {"t", "builtin:mcp", "Tool discovery"},
	} {
		if got := GroupFor(tt.name, tt.source); got != tt.want {
			t.Errorf("GroupFor(%q,%q)=%q want %q", tt.name, tt.source, got, tt.want)
		}
	}
}

func TestApplyToSavesOnlyChangedTools(t *testing.T) {
	cfg := Config{DisabledTools: map[string]bool{}, EnabledTools: map[string]bool{}, DisabledSkills: map[string]bool{}}
	m := &Menu{Tabs: [2][]Row{{
		{ID: "deferred", Name: "deferred"},        // inactive this session, untouched
		{ID: "bash", Name: "bash", Enabled: true}, // turned off below
		{ID: "grep", Name: "grep"},                // turned on below
		{ID: "keep", Name: "keep", Enabled: true}, // untouched
	}, nil}}
	m.MarkInitial()
	m.Tabs[0][1].Enabled = false
	m.Tabs[0][2].Enabled = true
	m.ApplyTo(&cfg)
	if cfg.DisabledTools["deferred"] || cfg.EnabledTools["deferred"] || cfg.DisabledTools["keep"] {
		t.Fatalf("untouched tools were written: %+v", cfg)
	}
	if !cfg.DisabledTools["bash"] || !cfg.EnabledTools["grep"] || cfg.DisabledTools["grep"] {
		t.Fatalf("changes not saved: %+v", cfg)
	}
}

func TestApplyToKeepsOmittedSkillsInRestrictedAllowlist(t *testing.T) {
	cfg := Config{DisabledTools: map[string]bool{}, EnabledTools: map[string]bool{}, DisabledSkills: map[string]bool{}}
	m := &Menu{RestrictSkills: true, KeepSkills: []string{"user-only"}, Tabs: [2][]Row{nil, {{ID: "model", Name: "model", Enabled: true}}}}
	m.ApplyTo(&cfg)
	if !slices.Contains(cfg.SkillAllowlist, "user-only") || !slices.Contains(cfg.SkillAllowlist, "model") {
		t.Fatalf("allowlist=%v", cfg.SkillAllowlist)
	}
}
