package toolsettingsext

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestEnabledDeferredToolStaysOutOfInitialRequest(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	cfg := toolsettings.Config{EnabledTools: map[string]bool{"ast_search": true, "tool_search": true}}
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), cfg); err != nil {
		t.Fatal(err)
	}
	settings, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	ast, err := filepath.Abs("../ast-search")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ready"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{settings, ast}, Prompt: "hello"})
	if result.ExitCode != 0 {
		t.Fatalf("session failed: %+v", result)
	}
	if slices.Contains(pigtest.ToolNames(mock), "ast_search") {
		t.Fatal("saved permission activated a deferred schema")
	}
}
