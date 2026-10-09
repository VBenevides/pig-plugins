package todoext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuidanceIsIdempotent(t *testing.T) {
	result, err := appendGuidance(sdk.Context{}, map[string]any{"systemPrompt": "original"})
	if err != nil {
		t.Fatal(err)
	}
	prompt := result.(map[string]any)["systemPrompt"].(string)
	if !strings.HasPrefix(prompt, "original") || strings.Count(prompt, "<Task_Management>") != 1 {
		t.Fatal(prompt)
	}
	again, err := appendGuidance(sdk.Context{}, map[string]any{"systemPrompt": prompt})
	if err != nil || again != nil {
		t.Fatal(again, err)
	}
	if _, err = appendGuidance(sdk.Context{}, map[string]any{}); err == nil {
		t.Fatal("missing prompt accepted")
	}
}
func TestPiGProviderReceivesGuidanceOnce(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("one"), pigtest.Text("two"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"first", "second"}})
	requests := mock.Requests()
	if len(requests) != 2 {
		t.Fatal(len(requests))
	}
	for _, request := range requests {
		prompt := ""
		for _, raw := range request["messages"].([]any) {
			message := raw.(map[string]any)
			if message["role"] == "system" {
				content, ok := message["content"].(string)
				if !ok {
					t.Fatalf("unexpected system content: %v", message)
				}
				prompt += content
			}
		}
		if strings.Count(prompt, "<Task_Management>") != 1 || !strings.Contains(prompt, "never IDs") || !strings.Contains(prompt, "append") {
			t.Fatalf("guidance missing/duplicated: %s", prompt)
		}
	}
}
