package smartapprovelancet_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestFusedApprovalDialogTUI(t *testing.T) {
	useFusedGuard(t)
	home := pigtest.NewHome(t)
	mustWrite(t, filepath.Join(home.AgentDir(), "settings.json"), `{}`)
	mustWrite(t, filepath.Join(home.AgentDir(), "check-update.json"), `{"enabled":false}`)
	mustWrite(t, filepath.Join(home.AgentDir(), "auto-model.json"), `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`)
	var replies []pigtest.Reply
	for _, name := range []string{"once", "deny", "q", "escape", "ctrl"} {
		replies = append(replies, writeCall(".env."+name, "allowed once"), pigtest.Text("settled "+name))
	}
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	script, err := filepath.Abs("testdata/approval_tui.py")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := filepath.Abs(os.Getenv("PIG_FUSED_TEST_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", script, binary)
	cmd.Dir = home.Work
	cmd.Env = home.Env(map[string]string{"TERM": "xterm-256color"})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("approval TUI: %v: %s", err, output)
	}
	if len(mock.Requests()) != 10 {
		t.Fatalf("requests=%d want 10", len(mock.Requests()))
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 5 {
		t.Fatal(results)
	}
	if strings.Contains(results[0], "smart-approve-lancet:") {
		t.Fatal("default Enter did not allow once", results[0])
	}
	for _, result := range results[1:] {
		if !strings.Contains(result, "user denied write") {
			t.Fatal(result)
		}
	}
	t.Log(string(output))
}
