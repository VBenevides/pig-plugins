package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// Cursor uses the host's thinking selection. Its provider-level test separately verifies concrete Grok IDs.
func TestHostThinkingDefaultPreservesSavedLevels(t *testing.T) {
	pigtest.UseFusedBinary(t)
	pigtest.RequirePig(t)
	for _, tt := range []struct{ name, settings, want string }{
		{"new", `{"disableUpdateCheck":true}`, "medium"},
		{"saved-global", `{"disableUpdateCheck":true,"defaultThinkingLevel":"high"}`, "high"},
		{"saved-model", `{"disableUpdateCheck":true,"defaultThinkingLevel":"high","modelThinkingLevels":{"mock/mock-model":"low"}}`, "low"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := pigtest.NewHome(t)
			if err := os.MkdirAll(home.AgentDir(), 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home.AgentDir(), "settings.json")
			if err := os.WriteFile(path, []byte(tt.settings), 0o600); err != nil {
				t.Fatal(err)
			}
			mock := pigtest.NewMockLLM(pigtest.Text("done"))
			defer mock.Close()
			result := home.RunRPC(t, mock, pigtest.RPCOptions{Reasoning: true, Prompts: []string{"hi"}})
			found := false
			for _, event := range result.Events {
				if event["type"] != "response" || event["command"] != "get_state" {
					continue
				}
				data, _ := event["data"].(map[string]any)
				if data["thinkingLevel"] != tt.want {
					t.Fatalf("thinkingLevel=%v want %s", data["thinkingLevel"], tt.want)
				}
				found = true
			}
			if !found {
				t.Fatal("get_state response missing")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var beforeSettings, afterSettings map[string]any
			if err := json.Unmarshal([]byte(tt.settings), &beforeSettings); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(after, &afterSettings); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"defaultThinkingLevel", "modelThinkingLevels"} {
				before, _ := json.Marshal(beforeSettings[key])
				after, _ := json.Marshal(afterSettings[key])
				if string(before) != string(after) {
					t.Fatalf("saved %s changed: %s -> %s", key, before, after)
				}
			}
		})
	}
}
