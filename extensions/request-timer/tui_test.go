package requesttimerext

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// This opt-in check needs tmux and a terminal-capable local pig. It uses an isolated tmux server and HOME.
func TestLiveWorkingIndicatorAndFinalElapsed(t *testing.T) {
	if os.Getenv("PIG_TIMER_TUI_TEST") != "1" {
		t.Skip("set PIG_TIMER_TUI_TEST=1 to run the isolated TUI check")
	}
	fused := pigtest.UseFusedBinary(t)
	pigtest.RequirePig(t)
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	pig, err := exec.LookPath("pig")
	if err != nil {
		t.Fatal(err)
	}
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Reply{Text: "timer test finished", Delay: 4500 * time.Millisecond})
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	socket := fmt.Sprintf("pig-timer-test-%d", os.Getpid())
	run := func(args ...string) (string, error) {
		cmd := exec.Command(tmux, append([]string{"-L", socket}, args...)...)
		cmd.Env = home.Env(nil)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	args := []string{"new-session", "-d", "-s", "timer", "-x", "100", "-y", "30", "-c", home.Work, pig, "--provider", "mock", "--model", "mock-model"}
	if !fused {
		args = append(args, "-e", extension)
	}
	if out, err := run(args...); err != nil {
		t.Fatalf("start TUI: %v %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := run("send-keys", "-t", "timer", "C-c", "C-c"); err != nil {
			t.Logf("request TUI shutdown: %v %s", err, out)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := run("has-session", "-t", "timer"); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		if out, err := run("kill-server"); err != nil {
			t.Logf("stop isolated tmux server: %v %s", err, out)
		}
	})
	wait := func(fragment string) string {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		var screen string
		for time.Now().Before(deadline) {
			var err error
			screen, err = run("capture-pane", "-p", "-t", "timer")
			if err != nil {
				t.Fatalf("capture TUI: %v %s", err, screen)
			}
			if strings.Contains(screen, fragment) {
				return screen
			}
			if fragment == "mock-model" && strings.Contains(screen, "Pick a theme.") {
				if out, err := run("send-keys", "-t", "timer", "Escape"); err != nil {
					t.Fatalf("skip setup: %v %s", err, out)
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("missing %q in TUI:\n%s", fragment, screen)
		return ""
	}
	wait("mock-model")
	if out, err := run("send-keys", "-t", "timer", "test timer", "Enter"); err != nil {
		t.Fatalf("send prompt: %v %s", err, out)
	}
	wait("Working - 00h 00m 01s")
	screen := wait("Elapsed - 00h 00m 04s")
	if strings.Contains(screen, "Working - ") {
		t.Fatalf("working label remains after completion:\n%s", screen)
	}
	if len(mock.Requests()) != 1 {
		t.Fatal("elapsed record triggered another model request")
	}
}
