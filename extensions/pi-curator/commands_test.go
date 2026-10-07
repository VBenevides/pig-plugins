package picurator_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
