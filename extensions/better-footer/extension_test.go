package betterfooter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestRPCSettingsTogglePersistsAndCancelPreserves(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	path := filepath.Join(home.AgentDir(), bf.SettingsFile)
	initial := bf.Settings{KeepRecentModel: true, SkipExhaustedScopedModels: false}
	if err := bf.SaveSettings(path, initial); err != nil {
		t.Fatal(err)
	}
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	selections := 0
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"/better-footer"}, Dialog: func(request map[string]any) map[string]any {
		if request["title"] == "Better footer settings" && selections == 0 {
			selections++
			options, ok := request["options"].([]any)
			if !ok || len(options) != 2 {
				t.Fatalf("invalid settings options: %v", request)
			}
			return map[string]any{"value": options[0]}
		}
		return map[string]any{"cancelled": true}
	}})
	saved, err := bf.LoadSettings(path)
	if err != nil || saved.KeepRecentModel || saved.SkipExhaustedScopedModels {
		t.Fatalf("settings %+v %v", saved, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"/better-footer"}, Dialog: func(map[string]any) map[string]any { return map[string]any{"cancelled": true} }})
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("cancel changed settings", err)
	}
	if len(mock.Requests()) != 0 {
		t.Fatal("settings called a model")
	}
}

type badgeHost struct{}

func (badgeHost) SetStatus(string, string) {}

func TestRendererKeepsAutoModelAndSmartApproveBadges(t *testing.T) {
	var statuses footerstatus.Store
	host := badgeHost{}
	modelBadge := "\x1b[32m🧠 primary\x1b[39m"
	approvalBadge := "smart-approve-lancet strict - lancet off"
	// Insert in reverse order to prove rendering orders by status key.
	statuses.Set(host, "smart-approve-lancet", approvalBadge)
	statuses.Set(host, "auto-model", modelBadge)
	state := bf.RenderState{Cwd: "/project", Provider: "test", Model: "primary-model"}
	render := func() string {
		state.Statuses = statuses.Snapshot()
		return strings.Join(bf.RenderFooter(state, 240, bf.Theme{}, time.Unix(1000, 0)), "\n")
	}
	for range 2 {
		output := render()
		if strings.Count(output, state.Model) != 1 {
			t.Fatalf("active model must appear exactly once: %q", output)
		}
		if !strings.Contains(output, modelBadge) || !strings.Contains(output, approvalBadge) {
			t.Fatalf("status badge lost on render: %q", output)
		}
		if strings.Index(output, modelBadge) >= strings.Index(output, approvalBadge) {
			t.Fatalf("status order is not deterministic: %q", output)
		}
	}
	statuses.Set(host, "auto-model-usage", "Checking quota…")
	if output := render(); !strings.Contains(output, "Checking quota…") || !strings.Contains(output, modelBadge) || !strings.Contains(output, approvalBadge) {
		t.Fatalf("new badge displaced persistent badges: %q", output)
	}
	statuses.Set(host, "auto-model-usage", "")
	if output := render(); strings.Contains(output, "Checking quota…") || !strings.Contains(output, modelBadge) || !strings.Contains(output, approvalBadge) {
		t.Fatalf("clearing temporary badge lost persistent badges: %q", output)
	}
}
