package readmap_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestBundledLiteralAndDialogTUI(t *testing.T) {
	binary := os.Getenv("PIG_READMAP_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set PIG_READMAP_SMOKE_BINARY")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("editor_tui.py")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"settings.json":     `{}`,
		"auto-model.json":   `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`,
		"check-update.json": `{"enabled":false}`,
	} {
		if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	question := pigtest.Calls(pigtest.Call("ask_user_question", map[string]any{"questions": []any{map[string]any{"header": "Fixture", "question": "Fixture choice?", "options": []any{map[string]any{"label": "Alpha", "description": "First"}, map[string]any{"label": "Beta", "description": "Second"}}}}}))
	mock := pigtest.NewMockLLM(pigtest.Text("literal accepted"), question, pigtest.Text("first cancelled"), question, pigtest.Text("second cancelled"), question, pigtest.Text("custom accepted"))
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", script, binary)
	cmd.Dir = home.Work
	cmd.Env = home.Env(map[string]string{"TERM": "xterm-256color"})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("editor TUI: %v: %s", err, output)
	}
	requests := mock.Requests()
	if len(requests) != 7 {
		t.Fatalf("requests=%d, want 7", len(requests))
	}
	data, _ := json.Marshal(requests[0]["messages"])
	if !strings.Contains(string(data), "/this-is-literal") {
		t.Fatalf("literal slash lost: %s", data)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 3 || results[0] != "User declined to answer questions" || results[1] != "User declined to answer questions" || !strings.Contains(results[2], `"Fixture choice?"="q"`) {
		t.Fatalf("dialog results=%q", results)
	}
	t.Log(string(output))
}
