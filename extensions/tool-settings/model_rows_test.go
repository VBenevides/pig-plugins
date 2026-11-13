package toolsettingsext

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestToolRowsSeparatePermissionAndVisibility(t *testing.T) {
	cfg := toolsettings.Config{DisabledTools: map[string]bool{}}
	for _, tc := range []struct {
		name                    string
		exposure                sdk.ToolExposure
		active, hidden, visible bool
		label                   string
	}{
		{"bash", sdk.ToolExposureDirect, true, false, true, "direct"},
		{"todo", sdk.ToolExposureDeferred, false, false, false, "deferred"},
		{"todo", sdk.ToolExposureDeferred, true, false, true, "deferred"},
		{"goal_finish", sdk.ToolExposureDirect, false, false, false, "conditional"},
	} {
		var active, hidden []string
		if tc.active {
			active = append(active, tc.name)
		}
		if tc.hidden {
			hidden = append(hidden, tc.name)
		}
		row, include, err := modelToolRow(sdk.ToolInfo{Name: tc.name, Exposure: tc.exposure, Parameters: json.RawMessage(`{"type":"object"}`)}, cfg, active, hidden, nil)
		if err != nil || !include || !row.Enabled || row.Visible != tc.visible || row.Exposure != tc.label {
			t.Fatalf("%s: row=%+v include=%v err=%v", tc.name, row, include, err)
		}
	}
}

func TestBackendRowsStayHiddenExceptForRecovery(t *testing.T) {
	cfg := toolsettings.Config{DisabledTools: map[string]bool{}}
	tool := sdk.ToolInfo{Name: "mcp__exa__web_search_exa", Exposure: sdk.ToolExposureDeferred}
	registered := map[string]bool{"web_search": true}
	_, include, err := modelToolRow(tool, cfg, nil, nil, registered)
	if err != nil || include {
		t.Fatalf("backend included=%v err=%v", include, err)
	}
	cfg.DisabledTools[tool.Name] = true
	row, include, err := modelToolRow(tool, cfg, nil, nil, registered)
	if err != nil || !include || row.Enabled || row.Exposure != "backend" {
		t.Fatalf("recovery row=%+v err=%v", row, err)
	}
}

func TestInvalidSchemaRemainsObservable(t *testing.T) {
	_, _, err := modelToolRow(sdk.ToolInfo{Name: "bad", Parameters: json.RawMessage(`{`)}, toolsettings.Config{}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "count tool bad") {
		t.Fatalf("err=%v", err)
	}
}

func TestUserOnlySkillsAreOmittedButLoadedBodiesRemainCounted(t *testing.T) {
	cfg := toolsettings.Config{Skills: []toolsettings.Skill{{Name: "model", Path: "model"}, {Name: "user", Path: "user", Hidden: true}, {Name: "disabled-user", Path: "disabled-user", Hidden: true}}, DisabledSkills: map[string]bool{toolsettings.Canonical("disabled-user"): true}}
	menu := &toolsettings.Menu{LoadedKnown: true, ShowHelp: true, RestrictSkills: true}
	addModelSkills(menu, cfg, map[string]int{"user": 100, "disabled-user": 20, "model": 10})
	if len(menu.Tabs[1]) != 1 || menu.Tabs[1][0].ID != "model" || menu.OtherLoadedChars != 120 {
		t.Fatalf("menu=%+v", menu)
	}
	if len(menu.KeepSkills) != 1 || menu.KeepSkills[0] != "user" {
		t.Fatalf("keep=%v", menu.KeepSkills)
	}
	text := strings.Join(menu.Render(96), "\n")
	if !strings.Contains(text, "Last request loaded skill blocks: 130 chars") {
		t.Fatal(text)
	}
}
