package toolsettings

import (
	"path/filepath"
	"testing"
)

func TestDefaultAllowsNonBrowserToolsRegardlessOfRecordedDefaults(t *testing.T) {
	names := []string{"tool_search", "todo", "web_search", "url_context", "mcp__parallel__web_search", "mcp__exa__web_search_exa", "mcp__exa__web_fetch_exa", "other_new_tool"}
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	menu := &Menu{}
	for _, name := range names {
		menu.Tabs[0] = append(menu.Tabs[0], Row{ID: name, DefaultEnabled: false})
		cfg.Defaults[name] = false
		cfg.DisabledTools[name] = true
	}
	menu.Tabs[0] = append(menu.Tabs[0], Row{ID: "browser_navigate", DefaultEnabled: true})
	menu.ApplyPreset(DefaultPreset)
	menu.ApplyTo(&cfg)
	if menu.presetLabel() != DefaultPreset {
		t.Fatal(menu.presetLabel())
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if !cfg.ToolEnabled(name) {
			t.Fatalf("default blocked %s", name)
		}
	}
	if cfg.ToolEnabled("browser_navigate") || cfg.ToolEnabled("browser_new_tool") {
		t.Fatal("default allowed browser tools")
	}
	cfg.EnabledTools["browser_navigate"] = true
	delete(cfg.DisabledTools, "browser_navigate")
	if !cfg.ToolEnabled("browser_navigate") {
		t.Fatal("explicit browser opt-in rejected")
	}
	cfg.Reset()
	for _, name := range names {
		if !cfg.ToolEnabled(name) {
			t.Fatalf("reset restored an obsolete denial for %s", name)
		}
	}
	if cfg.ToolEnabled("browser_navigate") {
		t.Fatal("reset preserved browser opt-in")
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProfileEnv, DefaultPreset)
	cfg, err = LoadEffective(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ToolEnabled("mcp__exa__web_search_exa") || cfg.ToolEnabled("browser_navigate") {
		t.Fatal("launch default disagrees with saved default")
	}
}
