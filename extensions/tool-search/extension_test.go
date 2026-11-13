package toolsearch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestDiscoveryUsesSavedSettings(t *testing.T) {
	pigtest.RequirePig(t)
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	for _, enabled := range []bool{true, false, true} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			settings := filepath.Join(home.AgentDir(), toolsettings.FileName)
			cfg, err := toolsettings.Load(settings)
			if err != nil {
				t.Fatal(err)
			}
			cfg.DisabledTools[toolName] = !enabled
			if err := toolsettings.Save(settings, cfg); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				mock := pigtest.NewMockLLM(pigtest.Text("ready"), pigtest.Text("next request"))
				defer mock.Close()
				result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{path}, Prompts: []string{"hello", "hello again"}})
				for _, event := range result.Events {
					if event["type"] == "extension_error" {
						t.Fatalf("extension failed: %v", event)
					}
				}
				requests := mock.Requests()
				if len(requests) != 2 {
					t.Fatalf("requests=%d", len(requests))
				}
				for _, request := range requests {
					var names []string
					for _, raw := range request["tools"].([]any) {
						names = append(names, raw.(map[string]any)["function"].(map[string]any)["name"].(string))
					}
					if slices.Contains(names, toolName) != enabled {
						t.Fatalf("enabled=%v tools=%v", enabled, names)
					}
				}
			}
		})
	}
}

func TestDisabledBuiltinReportsRemedy(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "settings.json"), []byte(`{"extensions":["-builtin:tool-search"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{path}, Prompts: []string{"hello"}})
	for _, notice := range result.Notices() {
		if strings.Contains(notice, "Tool discovery is unavailable") {
			return
		}
	}
	t.Fatalf("missing actionable diagnostic; events: %v; stderr: %s", result.Events, result.Stderr)
}
