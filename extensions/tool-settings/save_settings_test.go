package toolsettingsext

import (
	"reflect"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestLaunchProfileSaveChangesOnlyPromptStyle(t *testing.T) {
	previous := toolsettings.Config{PromptStyle: "default", DisabledTools: map[string]bool{"custom": true}, ToolAllowlist: []string{"custom"}, NativeFiles: true}
	effective := toolsettings.Config{PromptStyle: "default", DisabledTools: map[string]bool{"ask_user_question": true}}
	menu := &toolsettings.Menu{ProfileOverride: toolsettings.BenchmarkPreset, PromptStyle: "legacy"}
	actual := settingsToSave(menu, previous, effective)
	previous.PromptStyle = "legacy"
	if !reflect.DeepEqual(actual, previous) {
		t.Fatalf("saved launch permissions: %+v", actual)
	}
}
