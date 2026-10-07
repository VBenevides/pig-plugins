package picurator_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/curator"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func writeFile(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCommandsPersistAndSavedSettingsWin(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	file := filepath.Join(home.AgentDir(), "pi-curator.json")
	writeFile(t, file, `{"unrelated":{"n":9007199254740993}}`)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil), Prompts: []string{"/pi-curator prefetch 700", "/pi-curator startup off", "/pi-curator engine fts", "/pi-curator status", "/pi-curator prefetch 7"}})
	var notices []string
	for _, notice := range result.Notices() {
		if strings.HasPrefix(notice, "pi-curator:") || strings.HasPrefix(notice, "repository memory:") {
			notices = append(notices, notice)
		}
	}
	if len(notices) != 5 {
		t.Fatal(notices)
	}
	if !strings.Contains(notices[3], "prefetch: 700 (saved)") || !strings.Contains(notices[3], "startup: 0 (saved)") || !strings.Contains(notices[3], "engine: fts (saved)") {
		t.Fatal(notices[3])
	}
	if !strings.Contains(notices[4], "prefetch must be") {
		t.Fatal(notices[4])
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "9007199254740993") {
		t.Fatal(string(data))
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestDeferredToolsCanBeDiscovered(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	writeFile(t, filepath.Join(home.AgentDir(), "settings.json"), `{"defaultTools":["+tool_search"]}`)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "memory search repository history"})), pigtest.Calls(pigtest.Call("memory_search", map[string]any{"query": "atomic writes"})), pigtest.Text("done"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "recall atomic writes", Env: env(nil)})
	names := strings.Join(pigtest.ToolNames(mock), " ")
	if strings.Contains(names, "memory_search") {
		t.Fatal("memory_search was not deferred", names)
	}
	results := pigtest.ToolResults(mock)
	if result.ExitCode != 0 || len(results) != 2 || !strings.Contains(results[1], marker) {
		t.Fatal(result, results)
	}
}
func TestCaptureRedactsSecrets(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	mock := pigtest.NewMockLLM(pigtest.Text("recorded"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "Remember API_KEY=sk-secret-1234567890abcdefgh", Env: env(nil)})
	log := journal(home)
	if strings.Contains(log, "sk-secret-1234567890abcdefgh") {
		t.Fatal("secret persisted without redaction")
	}
	if !strings.Contains(log, "recorded") {
		t.Fatal("successful neighbor not captured", log)
	}
}

func TestOffOnImmediatelyControlsRecallToolsAndCapture(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	file := filepath.Join(home.AgentDir(), "pi-curator.json")
	writeFile(t, file, `{"prefetchBudget":512,"startupDecisions":2,"searchEngine":"legacy"}`)
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("memory_search", map[string]any{"query": "atomic writes"})),
		pigtest.Calls(pigtest.Call("memory_read", map[string]any{"ids": []string{"decision"}})),
		pigtest.Text("private-off-answer"),
		pigtest.Calls(pigtest.Call("memory_search", map[string]any{"query": "atomic writes"})),
		pigtest.Text("restored-on-answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil),
		Prompts: []string{"/pi-curator off", "private-off-prompt atomic writes", "/pi-curator on", "restored-on-prompt atomic writes"}})
	if len(result.Asked) != 0 {
		t.Fatal("on asked consent for existing consented memory", result.Asked)
	}
	toolResults := pigtest.ToolResults(mock)
	if len(toolResults) != 3 || !strings.Contains(toolResults[0], "disabled") || !strings.Contains(toolResults[1], "disabled") || !strings.Contains(toolResults[2], marker) {
		t.Fatal(toolResults, result.Stderr)
	}
	requests := mock.Requests()
	if len(requests) < 4 {
		t.Fatal("missing model requests", result.Stderr)
	}
	first, err := json.Marshal(requests[0])
	if err != nil || strings.Contains(string(first), marker) || strings.Contains(string(first), "Repository memory") {
		t.Fatal("off injected recall", string(first), err)
	}
	restored, err := json.Marshal(requests[3])
	if err != nil || !strings.Contains(string(restored), marker) || !strings.Contains(string(restored), "Repository memory") {
		t.Fatal("on did not restore automatic recall", string(restored), err)
	}
	log := journal(home)
	for _, private := range []string{"private-off-prompt", "private-off-answer"} {
		if strings.Contains(log, private) {
			t.Fatal("off captured private content", log)
		}
	}
	for _, preserved := range []string{marker, "restored-on-prompt", "restored-on-answer"} {
		if !strings.Contains(log, preserved) {
			t.Fatal("on did not restore capture or preserve journal", log)
		}
	}
	values, err := (curator.Config{Getenv: func(key string) string {
		if key == "PIG_CODING_AGENT_DIR" {
			return home.AgentDir()
		}
		return ""
	}}).Effective()
	if err != nil || !values.Enabled || values.Prefetch != "512" || values.Startup != "2" || values.Engine != "legacy" {
		t.Fatal(values, err)
	}
}

func TestOffPersistsAcrossRestartWithoutPromptRecallOrCapture(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	before := journal(home)
	mock := pigtest.NewMockLLM(pigtest.Text("private-restart-answer"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil), Prompts: []string{"/pi-curator off"}})
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)},
		Env:     env(map[string]string{"PI_CURATOR_PREFETCH_BUDGET": "512", "PI_CURATOR_STARTUP_DECISIONS": "2"}),
		Prompts: []string{"private-restart-prompt atomic writes"}, Confirm: func(map[string]any) bool { return true }})
	if len(result.Asked) != 0 || journal(home) != before || strings.Contains(requestText(mock), marker) {
		t.Fatal("disabled restart recalled, prompted, or captured", result.Asked, journal(home), requestText(mock))
	}
}

func TestOnDefersConsentAndHonorsEarlierRefusal(t *testing.T) {
	home := repoHome(t)
	writeFile(t, filepath.Join(home.AgentDir(), "pi-curator.json"), `{"enabled":false}`)
	mock := pigtest.NewMockLLM(pigtest.Text("private-disabled"), pigtest.Text("declined"), pigtest.Text("still-declined"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil),
		Prompts: []string{"private-disabled-prompt", "/pi-curator on", "ask-on-first-prompt", "/pi-curator off", "/pi-curator on", "do-not-ask-again"},
		Confirm: func(map[string]any) bool { return false }})
	if len(result.Asked) != 1 {
		t.Fatal("off prompted or on bypassed/refreshed refusal", result.Asked, result.Stderr)
	}
	if _, err := os.Stat(filepath.Join(home.Work, ".curator")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("on granted consent", err)
	}
	for _, name := range pigtest.ToolNames(mock) {
		if strings.HasPrefix(name, "memory_") {
			t.Fatal("disabled startup exposed memory tools", name)
		}
	}
}

func TestOnRestoresCaptureAfterExplicitConsent(t *testing.T) {
	home := repoHome(t)
	writeFile(t, filepath.Join(home.AgentDir(), "pi-curator.json"), `{"enabled":false}`)
	mock := pigtest.NewMockLLM(pigtest.Text("accepted-on-answer"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil),
		Prompts: []string{"/pi-curator on", "accepted-on-prompt"}, Confirm: func(map[string]any) bool { return true }})
	if len(result.Asked) != 1 || !strings.Contains(journal(home), "accepted-on-answer") || !strings.Contains(journal(home), "accepted-on-prompt") {
		t.Fatal(result.Asked, journal(home), result.Stderr)
	}
}
