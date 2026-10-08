package projectprompt

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/prompts"
)

func TestAppendPreservesBaseAndIsIdempotent(t *testing.T) {
	base := "host base\n<tools>live tools</tools>\n<skills>loaded skills</skills>"
	result, err := appendRules(sdk.Context{}, map[string]any{"systemPrompt": base})
	if err != nil {
		t.Fatal(err)
	}
	got := result.(map[string]any)["systemPrompt"].(string)
	if got != base+"\n\n"+prompts.ProjectSystem {
		t.Fatal("base prompt was not preserved exactly")
	}
	result, err = appendRules(sdk.Context{}, map[string]any{"systemPrompt": got})
	if err != nil || result != nil {
		t.Fatalf("already appended prompt changed: %v, %v", result, err)
	}
	// A new run must receive guidance even when the host starts from its base again.
	result, err = appendRules(sdk.Context{}, map[string]any{"systemPrompt": base})
	if err != nil || result == nil {
		t.Fatalf("new base did not receive rules: %v, %v", result, err)
	}
}

func TestMissingBaseFailsVisibly(t *testing.T) {
	for _, data := range []map[string]any{nil, {"systemPrompt": 12}} {
		if result, err := appendRules(sdk.Context{}, data); result != nil || err == nil {
			t.Fatalf("invalid base accepted: %v, %v", result, err)
		}
	}
}

// TestEffectivePrompt captures real host requests, not merely handler output.
func TestEffectivePrompt(t *testing.T) {
	pigtest.RequirePig(t)
	_, file, _, _ := runtime.Caller(0)
	extensions := filepath.Dir(filepath.Dir(file))
	loaded := []string{
		filepath.Join(extensions, "hashline-edit"),
		filepath.Join(extensions, "todo"),
		filepath.Join(extensions, "ask-user-question"),
		filepath.Join(extensions, "web-search"),
	}
	home := pigtest.NewHome(t)
	skillDir := filepath.Join(home.AgentDir(), "skills", "prompt-smoke")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: prompt-smoke\ndescription: HOST_SKILL_SENTINEL\n---\nSmoke skill.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "AGENTS.md"), []byte("HOST_PROJECT_CONTEXT_SENTINEL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: loaded, Prompts: []string{"baseline"}})
	baseline := mock.Requests()
	if len(baseline) != 1 {
		t.Fatalf("baseline request count: %d", len(baseline))
	}
	base := systemText(t, baseline[0])
	for _, required := range []string{"<tools>", "<rules>", "<docs>", "HOST_PROJECT_CONTEXT_SENTINEL", "<skills>", "HOST_SKILL_SENTINEL", home.Work} {
		if !strings.Contains(base, required) {
			t.Fatalf("baseline lacks %q", required)
		}
	}
	baseNames := toolNames(t, baseline[0])
	for _, name := range []string{"read", "edit", "write", "bash", "todo", "ask_user_question", "web_search"} {
		if !slices.Contains(baseNames, name) {
			t.Fatalf("baseline lacks tool %q: %v", name, baseNames)
		}
	}
	loaded = append(loaded, filepath.Join(extensions, "project-prompt"))
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: loaded, Prompts: []string{"first adapted run", "second adapted run"}})
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("effective request count: %d", len(requests))
	}
	for i, request := range requests[1:] {
		got := systemText(t, request)
		if got != base+"\n\n"+prompts.ProjectSystem {
			t.Fatalf("run %d did not preserve exact baseline and append rules", i+1)
		}
		if strings.Count(got, "<pig_plugins_project_rules>") != 1 {
			t.Fatalf("run %d has duplicate rules", i+1)
		}
		if names := toolNames(t, request); !slices.Equal(names, baseNames) {
			t.Fatalf("tools changed: %v, baseline %v", names, baseNames)
		}
	}
	t.Logf("2 effective requests preserved all %d baseline bytes, appended rules exactly once, and retained tools %v", len(base), baseNames)
}

func systemText(t *testing.T, request map[string]any) string {
	t.Helper()
	messages, ok := request["messages"].([]any)
	if !ok {
		t.Fatal("request has no messages")
	}
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] == "system" {
			text, ok := message["content"].(string)
			if !ok {
				t.Fatal("system content is not text")
			}
			return text
		}
	}
	t.Fatal("request has no system message")
	return ""
}

func toolNames(t *testing.T, request map[string]any) []string {
	t.Helper()
	tools, ok := request["tools"].([]any)
	if !ok {
		t.Fatal("request has no tools")
	}
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		if name == "" {
			t.Fatal("tool has no function name")
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
