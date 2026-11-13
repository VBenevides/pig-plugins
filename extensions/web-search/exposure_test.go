package websearchext

import (
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func TestNoDiscoveryFallbackRespectsDenial(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), toolsettings.Config{DisabledTools: map[string]bool{"web_search": true}}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := searchMCPFixture(t, "web_search", false, &calls)
	defer server.Close()
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ready"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}, Prompt: "hello", Env: map[string]string{"PIG_PARALLEL_MCP_URL": server.URL, "PIG_EXA_MCP_URL": server.URL}})
	if result.ExitCode != 0 {
		t.Fatalf("session failed: %+v", result)
	}
	if slices.Contains(pigtest.ToolNames(mock), "web_search") {
		t.Fatal("fallback exposed a denied wrapper")
	}
}
