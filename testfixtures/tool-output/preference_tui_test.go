package tooloutput_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestBundledToolOutputPreferenceTUI(t *testing.T) {
	for _, mode := range []string{"regular", "fullscreen"} {
		t.Run(mode, func(t *testing.T) { testBundledToolOutputPreference(t, mode) })
	}
}

func testBundledToolOutputPreference(t *testing.T, mode string) {
	binary := os.Getenv("PIG_TOOL_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set PIG_TOOL_SMOKE_BINARY to the bundled development executable")
	}
	if runtime.GOOS != "linux" {
		t.Skip("terminal fixture requires Linux")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("terminal fixture requires Python 3")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("bash", map[string]any{"command": "cat tool-output.txt"})), pigtest.Text("tool-preview-done"))
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	settings := filepath.Join(home.AgentDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(fmt.Sprintf(`{"tuiMode":%q,"enableInstallTelemetry":false}`, mode)), 0600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&output, "TOOL-ROW-%02d\n", i)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "tool-output.txt"), []byte(output.String()), 0600); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	ctx, cancel := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, filepath.Join(filepath.Dir(file), "preference_tui.py"), binary, settings)
	cmd.Dir = home.Work
	cmd.Env = home.Env(map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "", "PI_IMAGE_PROTOCOL": "none"})
	result, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tool preference TUI: %v\n%s", err, result)
	}
	if requests := len(mock.Requests()); requests != 2 {
		t.Fatalf("requests=%d, want tool call and answer only", requests)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 1 || strings.Count(results[0], "TOOL-ROW-") != 30 {
		t.Fatal("preview limit truncated the model's tool result")
	}
	t.Log(string(result))
}
