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
