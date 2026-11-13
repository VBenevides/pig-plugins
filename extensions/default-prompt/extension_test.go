package defaultprompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

// The probe appends instructions after the built-in default.
func TestDefaultPromptAndOldSettingsReachMockProvider(t *testing.T) {
	pigtest.RequirePig(t)
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := filepath.Abs("../adhd-output/testdata/session-probe")
	if err != nil {
		t.Fatal(err)
	}
	builtIn := map[string]string{"default": "You are a coding assistant inside pi.", "legacy": "You are an expert coding assistant operating inside pi"}
	for _, selected := range []string{"default", "legacy"} {
		for _, userSystem := range []string{"", "USER_SYSTEM_SENTINEL"} {
			t.Run(selected+"-user:"+userSystem, func(t *testing.T) {
				home := pigtest.NewHome(t)
				if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
					t.Fatal(err)
				}
				if userSystem != "" {
					if err := os.WriteFile(filepath.Join(home.AgentDir(), "SYSTEM.md"), []byte(userSystem), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(home.AgentDir(), toolsettings.FileName), []byte(`{"promptStyle":"`+selected+`"}`), 0600); err != nil {
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
					result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extension, probe}, Prompt: "hello", Env: env})
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
					if !strings.Contains(string(raw), "PROJECT_STYLE_SENTINEL") || !strings.Contains(string(raw), "UNCHANGED_EXTENSION_SENTINEL") {
						t.Fatal("style selection removed project or extension instructions")
					}
					if userSystem != "" {
						if !strings.Contains(string(raw), userSystem) || strings.Contains(string(raw), builtIn["default"]) || strings.Contains(string(raw), builtIn["legacy"]) {
							t.Fatalf("user SYSTEM.md did not replace the built-in prompt: %s", raw)
						}
						continue
					}
					if !strings.Contains(string(raw), builtIn["default"]) || strings.Contains(string(raw), builtIn["legacy"]) {
						t.Fatalf("default instructions absent: %s", raw)
					}
					if !strings.Contains(string(raw), "## Project conventions") {
						t.Fatal("project rules absent")
					}
				}
			})
		}
	}
}
