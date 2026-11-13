package hashlineedit_test

import (
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	"path/filepath"
	"testing"
)

func selectHashline(t *testing.T, home *pigtest.Home) {
	t.Helper()
	cfg, err := toolsettings.Load(filepath.Join(home.AgentDir(), toolsettings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg.NativeFiles = false
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), cfg); err != nil {
		t.Fatal(err)
	}
}
