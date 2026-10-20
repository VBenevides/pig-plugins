package commandoutput_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestNativeBashHookAndBypass(t *testing.T) {
	binary := os.Getenv("PIG_COMMAND_OUTPUT_SMOKE_BINARY")
	fused := os.Getenv("PIG_FUSED_TEST_BINARY") != ""
	if fused {
		binary = os.Getenv("PIG_FUSED_TEST_BINARY")
	}
	if binary == "" {
		t.Skip("set PIG_COMMAND_OUTPUT_SMOKE_BINARY to a host built with the reducer settings patch")
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
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "settings.json"), []byte(`{"reduceCommandOutput":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package demo
import ("fmt"; "os"; "testing")
func TestMany(t *testing.T) {
 f,err:=os.OpenFile("runs",os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if err!=nil{t.Fatal(err)}
 if _,err:=f.WriteString("run\n");err!=nil{t.Fatal(err)};if err:=f.Close();err!=nil{t.Fatal(err)}
 for i:=0;i<100;i++ {t.Run(fmt.Sprint(i),func(t *testing.T){})}
 if os.Getenv("FIXTURE_FAIL")=="1" {t.Fatal("fixture command failed")}
}
`
	if err := os.WriteFile(filepath.Join(home.Work, "demo_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	command := "GOWORK=off GO111MODULE=off go test -v ."
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": command})),
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": "PI_RTK_BYPASS=1 " + command})),
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": `cat "$TMPDIR"/pig-rtk-*/output-*.txt`})),
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": "FIXTURE_FAIL=1 " + command})),
		pigtest.Text("done"))
	defer mock.Close()
	extensions := []string{extension}
	if fused {
		extensions = nil
		for name, text := range map[string]string{
			"auto-model.json":   `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`,
			"check-update.json": `{"enabled":false}`,
		} {
			if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: extensions, Env: map[string]string{"PI_RTK": "0", "PI_RTK_BYPASS": "0", "TMPDIR": home.Work}})
	if result.ExitCode != 0 {
		t.Fatalf("%d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 4 || !strings.Contains(results[0], "Raw capture:") || strings.Contains(results[0], "=== RUN") || !strings.Contains(results[1], "=== RUN") || strings.Contains(results[1], "[RTK:") || !strings.Contains(results[2], "=== RUN   TestMany/99") || !strings.Contains(results[3], "fixture command failed") || !strings.Contains(results[3], "FAIL") {
		t.Fatalf("results: %q", results)
	}
	runs, err := os.ReadFile(filepath.Join(home.Work, "runs"))
	if err != nil || string(runs) != "run\nrun\nrun\n" {
		t.Fatalf("commands rerun or skipped: %q %v", runs, err)
	}
	for _, name := range pigtest.ToolNames(mock) {
		if name == "command-output" {
			t.Fatal("unexpected extra execution tool")
		}
	}
}
