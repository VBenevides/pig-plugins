package askmodeext

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestModeCommandSwitchesAndPreservesStateOnInvalidArguments(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{path},
		Prompts:    []string{"/mode", "/mode status", "/mode act", "/mode ASK", "/mode invalid", "/mode status", "/mode"},
	})
	want := []string{"Mode: ASK (read-only)", "Mode: ASK (read-only)", "Mode: ACT", "Mode: ASK (read-only)", "usage: /mode [act|ask|status]", "Mode: ASK (read-only)", "Mode: ACT"}
	if got := result.Notices(); !slices.Equal(got, want) {
		t.Fatalf("notices = %q, want %q; stderr: %s", got, want, result.Stderr)
	}
	if got := result.Statuses(Name); !slices.Equal(got, []string{"", "ASK", "ASK", "", "ASK", "ASK", ""}) {
		t.Fatalf("mode statuses = %q", got)
	}
	for _, event := range result.Events {
		if event["type"] == "extension_error" || event["notifyType"] == "error" {
			t.Fatalf("extension failed: %v", event)
		}
	}
	if strings.Contains(result.Stderr, "Error:") {
		t.Fatal(result.Stderr)
	}
}
