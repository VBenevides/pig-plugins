package automodels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestPickerSearchAndCancel(t *testing.T) {
	p := &picker{items: []choice{{"a", "anthropic/claude-opus", ""}, {"b", "openai-codex/gpt-5.5", ""}}, searchable: true}
	p.filter()
	for _, input := range []string{"c", "o", "d", "e", "x"} {
		if _, err := p.HandleInput(input); err != nil {
			t.Fatal(err)
		}
	}
	result, err := p.HandleInput("\r")
	if err != nil || !result.Done || result.Value != "b" {
		t.Fatalf("fuzzy selection: %+v %v", result, err)
	}
	result, err = p.HandleInput("\x1b")
	if err != nil || !result.Done || result.Value != nil {
		t.Fatalf("cancel: %+v %v", result, err)
	}
	p.query = "códex"
	p.items = []choice{{"unicode", "CÓDEX", ""}}
	p.filter()
	if len(p.matches) != 1 {
		t.Fatal("Unicode case-insensitive search failed")
	}
}

func TestRPCConfigurationPersistsAndCancelPreserves(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	store := quota.Store{Dir: home.AgentDir()}
	original := quota.Config{Primary: &quota.Slot{Provider: "old", Model: "primary", Thinking: "high"}, Fallback: &quota.Slot{Provider: "keep", Model: "fallback", Thinking: "low"}}
	if err := store.SaveConfig(original); err != nil {
		t.Fatal(err)
	}
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"/auto-model"}, Dialog: func(request map[string]any) map[string]any {
		title, _ := request["title"].(string)
		options, _ := request["options"].([]any)
		switch title {
		case "Configure Auto Model":
			return map[string]any{"value": options[0]}
		case "Select Primary model":
			return map[string]any{"value": "mock/mock-model"}
		case "Select Thinking Level":
			return map[string]any{"value": "medium"}
		}
		return map[string]any{"cancelled": true}
	}})
	config, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Primary == nil || *config.Primary != (quota.Slot{Provider: "mock", Model: "mock-model", Thinking: "medium"}) || config.Fallback == nil || *config.Fallback != *original.Fallback {
		t.Fatalf("wrong saved slots: %+v; notices=%v", config, result.Notices())
	}
	before, err := os.ReadFile(filepath.Join(home.AgentDir(), "auto-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"/auto-model"}, Dialog: func(map[string]any) map[string]any { return map[string]any{"cancelled": true} }})
	after, err := os.ReadFile(filepath.Join(home.AgentDir(), "auto-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("cancelling rewrote configuration")
	}
}

func TestRPCUsageMissingAuthAndPassiveQuota(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	store := quota.Store{Dir: home.AgentDir()}
	if err := store.SaveRateLimits(map[string]quota.RateLimitInfo{"anthropic": {Utilization: "0.42", Status: "allowed", WeeklyUtilization: "0.18"}}); err != nil {
		t.Fatal(err)
	}
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"/usage"}})
	text := strings.Join(result.Notices(), "\n")
	for _, want := range []string{"Account (anthropic)", "Account (openai-codex)", "Not logged in", "42%", "18%", "Quota unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if len(mock.Requests()) != 0 {
		t.Fatal("usage command unexpectedly called the model")
	}
}
