package toolsettingsext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	"testing"
)

func TestHiddenBrowserToolsStayConfigurableAndDisabled(t *testing.T) {
	row, shown, err := modelToolRow(sdk.ToolInfo{Name: "browser_navigate", Exposure: sdk.ToolExposureHidden}, toolsettings.Config{}, nil, []string{"browser_navigate"}, nil)
	if err != nil || !shown || row.Group != "Deferred" || row.Enabled || row.Visible {
		t.Fatalf("row=%+v shown=%v err=%v", row, shown, err)
	}
	_, shown, err = modelToolRow(sdk.ToolInfo{Name: "other_hidden", Exposure: sdk.ToolExposureHidden}, toolsettings.Config{}, nil, []string{"other_hidden"}, nil)
	if err != nil || shown {
		t.Fatal("unrelated hidden tool was exposed in settings")
	}
}

func TestDeferredToolsShareCategory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		exposure sdk.ToolExposure
		group    string
	}{
		{"todo", sdk.ToolExposureDeferred, "Deferred"},
		{"web_search", sdk.ToolExposureDeferred, "Deferred"},
		{"goal_finish", sdk.ToolExposureDeferred, "Deferred"},
		{"mcp__example__query", sdk.ToolExposureDeferred, "Deferred"},
		{"helper", sdk.ToolExposureCodemode, "Deferred"},
		{"read", sdk.ToolExposureDirect, "Files"},
		{"bash", sdk.ToolExposureDirect, "Shell"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, active := range [][]string{nil, {tc.name}} {
				row, visible, err := modelToolRow(sdk.ToolInfo{Name: tc.name, Exposure: tc.exposure}, toolsettings.Config{}, active, nil, nil)
				if err != nil || !visible || row.Group != tc.group {
					t.Fatalf("row=%+v visible=%v err=%v", row, visible, err)
				}
				if row.Visible != (len(active) > 0) {
					t.Fatal("grouping changed activation state")
				}
			}
		})
	}
}
