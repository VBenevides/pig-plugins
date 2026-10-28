package contextprune

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestProviderRequestRetainsUserTextAroundSkill(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	envelope := `<skill name="review" location="/skills/review/SKILL.md">` + "\n" + strings.Repeat("RULE_BODY_", 3000) + "\n</skill>"
	prompts := []string{envelope + "\nUNIQUE_ORIGINAL_REQUEST"}
	for range 6 {
		prompts = append(prompts, "next interaction")
	}
	prompts = append(prompts, envelope+"\nSECOND_REQUEST")
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: prompts})
	requests := mock.Requests()
	if len(requests) != len(prompts) {
		t.Fatalf("requests %d; stderr %s", len(requests), result.Stderr)
	}
	data, err := json.Marshal(requests[len(requests)-1]["messages"])
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[pruned: skill review") || !strings.Contains(text, "UNIQUE_ORIGINAL_REQUEST") || !strings.Contains(text, "SECOND_REQUEST") {
		t.Fatal("provider request lost surrounding user text or did not prune")
	}
}

func TestSkillLifetimeCommandsUseTheActiveBranch(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	envelope := `<skill name="review" location="/x">` + "\nrules\n</skill>"
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{
		envelope + "\nrequest", "/context-prune skills list", "/context-prune skills keep", "/context-prune skills finish", "/context-prune skills list", "/context-prune skills finish missing",
	}, Dialog: func(request map[string]any) map[string]any {
		options, _ := request["options"].([]any)
		if len(options) == 0 {
			t.Fatalf("missing invocation choices: %v", request)
		}
		return map[string]any{"value": options[0]}
	}})
	notices := strings.Join(result.Notices(), "\n")
	for _, want := range []string{"review [unknown]", "is retained", "is finished", "review [finished]", "not in the active branch"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("missing %q: %s; stderr %s", want, notices, result.Stderr)
		}
	}
}

func TestCancelledSkillSelectionDoesNotChangeState(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{
		`<skill name="review" location="/x">` + "\nrules\n</skill>", "/context-prune skills finish", "/context-prune skills list",
	}})
	notices := strings.Join(result.Notices(), "\n")
	if !strings.Contains(notices, "no skill state changed") || !strings.Contains(notices, "review [unknown]") {
		t.Fatalf("cancel changed state: %s", notices)
	}
}
