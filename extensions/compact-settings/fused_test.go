package compactsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// Exercise the bundled command without loading a second copy via -e.
func TestFusedCompactionLimitSurvivesRestart(t *testing.T) {
	binary := os.Getenv("PIG_FUSED_TEST_BINARY")
	if binary == "" {
		t.Skip("set PIG_FUSED_TEST_BINARY to a candidate fused binary")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "pig")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	for name, text := range map[string]string{
		"auto-model.json":   `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`,
		"check-update.json": `{"enabled":false}`,
	} {
		if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, restart := range []bool{false, true} {
		mock := pigtest.NewMockLLM(pigtest.Reply{Text: "first", PromptTokens: nativeThreshold - 10000})
		prompts := []string{"first prompt " + strings.Repeat("history ", 200), "second prompt"}
		if !restart {
			prompts = append([]string{"/compact-settings limit absolute 50k"}, prompts...)
		}
		result := home.RunRPC(t, mock, pigtest.RPCOptions{Prompts: prompts})
		requests := len(mock.Requests())
		mock.Close()
		if requests != 3 {
			t.Fatalf("restart=%v requests=%d, want two generations and one summary; notices=%q stderr=%s", restart, requests, result.Notices(), result.Stderr)
		}
		if !restart && !strings.Contains(strings.Join(result.Notices(), "\n"), "limit absolute: 50k") {
			t.Fatalf("limit command was not handled: %q", result.Notices())
		}
	}
}
