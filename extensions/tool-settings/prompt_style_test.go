package toolsettingsext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	agentprompts "github.com/VBenevides/pig-plugins/prompts/agent"
)

func TestPromptStylesReachMockProviderWithoutChangingInstalledFiles(t *testing.T) {
	pigtest.RequirePig(t)
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, installed := range []string{"default", "legacy"} {
		for _, selected := range []string{"default", "legacy"} {
			t.Run(installed+"-to-"+selected, func(t *testing.T) {
				home := pigtest.NewHome(t)
				if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"SYSTEM.md", "APPEND_SYSTEM.md", "AGENTS.md"} {
					text, err := agentprompts.Content(installed, name)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), toolsettings.Config{PromptStyle: selected}); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home.Work, "AGENTS.md"), []byte("PROJECT_STYLE_SENTINEL"), 0600); err != nil {
					t.Fatal(err)
				}
				for restart := 0; restart < 2; restart++ {
					mock := pigtest.NewMockLLM(pigtest.Text("ready"))
					defer mock.Close()
					env := map[string]string{}
					if restart == 1 {
						env[toolsettings.ProfileEnv] = toolsettings.BenchmarkPreset
					}
					result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extension}, Prompt: "hello", Env: env})
					if result.ExitCode != 0 {
						t.Fatalf("session failed: %+v", result)
					}
					requests := mock.Requests()
					if len(requests) != 1 {
						t.Fatalf("requests=%d", len(requests))
					}
					raw, err := json.Marshal(requests[0])
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(raw), "PROJECT_STYLE_SENTINEL") {
						t.Fatal("style selection removed project instructions")
					}
					expected := "You are a coding assistant inside pi."
					excluded := "You are an expert coding assistant operating inside pi"
					if selected == "legacy" {
						expected, excluded = excluded, expected
					}
					if !strings.Contains(string(raw), expected) || strings.Contains(string(raw), excluded) {
						t.Fatalf("wrong rendered style for %s: %s", selected, raw)
					}
					if selected == "legacy" && !strings.Contains(string(raw), "## Naming and structure") {
						t.Fatal("legacy project rules absent")
					}
					if selected == "default" && !strings.Contains(string(raw), "## Project conventions") {
						t.Fatal("default project rules absent")
					}
				}
				for _, name := range []string{"SYSTEM.md", "APPEND_SYSTEM.md", "AGENTS.md"} {
					expected, err := agentprompts.Content(installed, name)
					if err != nil {
						t.Fatal(err)
					}
					actual, err := os.ReadFile(filepath.Join(home.AgentDir(), name))
					if err != nil {
						t.Fatal(err)
					}
					if string(actual) != expected {
						t.Fatalf("style selection rewrote installed %s", name)
					}
				}
			})
		}
	}
}

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
