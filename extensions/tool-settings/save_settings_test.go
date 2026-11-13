package toolsettingsext

import (
	"reflect"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestLaunchProfileSavePreservesSavedSettings(t *testing.T) {
	previous := toolsettings.Config{DisabledTools: map[string]bool{"custom": true}, ToolAllowlist: []string{"custom"}, NativeFiles: true}
	effective := toolsettings.Config{DisabledTools: map[string]bool{"ask_user_question": true}}
	menu := &toolsettings.Menu{ProfileOverride: toolsettings.BenchmarkPreset}
	actual := settingsToSave(menu, previous, effective)
	if !reflect.DeepEqual(actual, previous) {
		t.Fatalf("saved launch permissions: %+v", actual)
	}
}
