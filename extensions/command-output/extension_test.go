package commandoutput_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestNativeBashHookAndBypass(t *testing.T) {
	pigtest.RequirePig(t)
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	source := `package demo
import ("fmt"; "os"; "testing")
func TestMany(t *testing.T) {
 f,err:=os.OpenFile("runs",os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if err!=nil{t.Fatal(err)}
 if _,err:=f.WriteString("run\n");err!=nil{t.Fatal(err)};if err:=f.Close();err!=nil{t.Fatal(err)}
 for i:=0;i<100;i++ {t.Run(fmt.Sprint(i),func(t *testing.T){})}
}
`
	if err := os.WriteFile(filepath.Join(home.Work, "demo_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	command := "GOWORK=off GO111MODULE=off go test -v ."
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": command})),
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": "PI_RTK_BYPASS=1 " + command})),
		pigtest.Text("done"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extension}, Env: map[string]string{"PI_RTK": "1", "PI_RTK_BYPASS": "0"}})
	if result.ExitCode != 0 {
		t.Fatalf("%d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 2 || !strings.Contains(results[0], "Raw capture:") || strings.Contains(results[0], "=== RUN") || !strings.Contains(results[1], "=== RUN") || strings.Contains(results[1], "[RTK:") {
		t.Fatalf("results: %q", results)
	}
	runs, err := os.ReadFile(filepath.Join(home.Work, "runs"))
	if err != nil || string(runs) != "run\nrun\n" {
		t.Fatalf("commands rerun or skipped: %q %v", runs, err)
	}
	for _, name := range pigtest.ToolNames(mock) {
		if name == "command-output" {
			t.Fatal("unexpected extra execution tool")
		}
	}
}
