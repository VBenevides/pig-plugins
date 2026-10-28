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
	prompts := []string{envelope + "\nUNIQUE_ORIGINAL_REQUEST", "/context-prune skills finish"}
	for range 6 {
		prompts = append(prompts, "next interaction")
	}
	prompts = append(prompts, "SECOND_REQUEST")
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: prompts, Dialog: func(request map[string]any) map[string]any {
		return map[string]any{"value": request["options"].([]any)[0]}
	}})
	requests := mock.Requests()
	if len(requests) != len(prompts)-1 {
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

func TestSkillExpiryPolicyAtTheProviderBoundary(t *testing.T) {
	for _, scenario := range []struct {
		action, expiry string
		pruned         bool
	}{
		{"release", "3", true}, {"release", "off", false}, {"active", "3", false}, {"keep", "3", false}, {"", "3", false},
	} {
		t.Run(scenario.action+scenario.expiry, func(t *testing.T) {
			pigtest.RequirePig(t)
			home := pigtest.NewHome(t)
			mock := pigtest.NewMockLLM()
			defer mock.Close()
			envelope := `<skill name="review" location="/x">` + "\n" + strings.Repeat("OLD_SKILL_BODY_", 2000) + "\n</skill>\nKEEP_REQUEST"
			prompts := []string{envelope, "/context-prune skills expire-after " + scenario.expiry}
			if scenario.action != "" {
				prompts = append(prompts, "/context-prune skills "+scenario.action)
			}
			for range 7 {
				prompts = append(prompts, "another user interaction")
			}
			prompts = append(prompts, "/context-prune status")
			result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: prompts, Dialog: func(request map[string]any) map[string]any {
				return map[string]any{"value": request["options"].([]any)[0]}
			}})
			requests := mock.Requests()
			data, err := json.Marshal(requests[len(requests)-1]["messages"])
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, "[pruned: skill review") != scenario.pruned || !strings.Contains(text, "KEEP_REQUEST") {
				t.Fatalf("wrong skill pruning: %s; stderr %s", scenario.action, result.Stderr)
			}
			if !strings.Contains(strings.Join(result.Notices(), "\n"), "skill bodies:") {
				t.Fatal("missing retention counts")
			}
		})
	}
}

func TestExplicitInteractionExcludesToolAndExtensionActivity(t *testing.T) {
	for _, source := range []string{"rpc", "interactive", "extension", ""} {
		got := explicitInteraction(map[string]any{"source": source, "text": "new request"})
		if got != (source == "rpc" || source == "interactive") {
			t.Fatal("wrong interaction provenance")
		}
	}
	for _, data := range []map[string]any{{"source": "rpc", "text": "/command"}, {"source": "rpc", "text": " "}, {"source": "rpc", "text": "correction", "streamingBehavior": "steer"}} {
		if explicitInteraction(data) {
			t.Fatal("non-interaction counted")
		}
	}
}
