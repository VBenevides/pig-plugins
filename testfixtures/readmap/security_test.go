package readmap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestBundledAdmissionAndStaleAuthority(t *testing.T) {
	binary := os.Getenv("PIG_READMAP_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set PIG_READMAP_SMOKE_BINARY to a candidate fused binary")
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
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"auto-model.json":   `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`,
		"check-update.json": `{"enabled":false}`,
	} {
		if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := "package example\nfunc Example() { println(1) }\n"
	if err := os.WriteFile(filepath.Join(home.Work, "example.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home.Work, "escape")); err != nil {
		t.Fatal(err)
	}
	edit := map[string]any{"path": "example.go", "version": hashline.ContentVersion([]byte(source)), "edits": []any{map[string]any{"op": "replace", "anchor": hashline.FormatAnchor(2, "func Example() { println(1) }"), "lines": []any{"func Example() { println(2) }"}}}}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("bash", map[string]any{"command": "printf forbidden > ask-shell"})),
		pigtest.Calls(pigtest.Call("write", map[string]any{"path": "ask-write", "content": "forbidden"})),
		pigtest.Text("ASK admission checked"),
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "example.go"})),
		pigtest.Calls(pigtest.Call("edit", edit)),
		pigtest.Calls(pigtest.Call("edit", edit)),
		pigtest.Calls(pigtest.Call("write", map[string]any{"path": "escape/forbidden", "content": "forbidden"})),
		pigtest.Text("ACT authority checked"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Prompts: []string{"/mode ask", "check denied actions", "/mode act", "check editing authority"}})
	outputs := pigtest.ToolResults(mock)
	if len(outputs) != 6 || !strings.Contains(outputs[0], "blocked") || !strings.Contains(outputs[1], "blocked") || !strings.Contains(outputs[2], "Version:") || !strings.Contains(outputs[3], "Applied 1 edit") || !strings.Contains(outputs[4], "edit rejected") || !strings.Contains(strings.ToLower(outputs[5]), "symlink") {
		t.Fatalf("admission results: %q; stderr=%s", outputs, result.Stderr)
	}
	for _, path := range []string{filepath.Join(home.Work, "ask-shell"), filepath.Join(home.Work, "ask-write"), filepath.Join(outside, "forbidden")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("denied side effect exists or could not be checked: %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(home.Work, "example.go"))
	if err != nil || string(data) != "package example\nfunc Example() { println(2) }\n" {
		t.Fatalf("approved edit missing or stale edit changed data: %q: %v", data, err)
	}
}
