package toolsearch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestDefaultAndCommand(t *testing.T) {
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
		Prompts:    []string{"/tool-search", "/tool-search off", "/tool-search off", "/tool-search", "/tool-search ON", "/tool-search invalid", "/tool-search"},
	})
	want := []string{"Tool search: on", "Tool search: off", "Tool search: off", "Tool search: off", "Tool search: on", "usage: /tool-search [on|off]", "Tool search: on"}
	if got := result.Notices(); !slices.Equal(got, want) {
		t.Fatalf("notices = %q, want %q; stderr: %s", got, want, result.Stderr)
	}
	for _, event := range result.Events {
		if event["type"] == "extension_error" || event["notifyType"] == "error" {
			t.Fatalf("extension failed: %v", event)
		}
	}
}

func TestDisabledBuiltinReportsRemedy(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "settings.json"), []byte(`{"extensions":["-builtin:tool-search"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("unused"))
	defer mock.Close()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{path}, Prompts: []string{"/tool-search on"}})
	for _, event := range result.Events {
		if strings.Contains(fmt.Sprint(event), "tool_search is unavailable; enable the built-in tool-search extension") {
			return
		}
	}
	t.Fatalf("missing actionable diagnostic; events: %v; stderr: %s", result.Events, result.Stderr)
}
