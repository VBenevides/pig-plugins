package hashlineedit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/guard"
	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestCoordinatedWriteReplacesBuiltinAndPreservesGuard(t *testing.T) {
	pigtest.RequirePig(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := filepath.Abs("../smart-approve-lancet")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	edit := map[string]any{"path": "new.txt", "version": hashline.ContentVersion([]byte("created\n")), "edits": []any{
		map[string]any{"op": "replace", "anchor": anchor(1, "created"), "lines": []any{"edited"}},
	}}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("write", map[string]any{"path": "new.txt", "content": "created\n"})),
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "new.txt"})),
		pigtest.Calls(pigtest.Call("edit", edit)),
		pigtest.Calls(pigtest.Call("write", map[string]any{"path": "new.txt", "content": "explicit overwrite\n"})),
		pigtest.Calls(pigtest.Call("edit", edit)),
		pigtest.Calls(pigtest.Call("write", map[string]any{"path": ".env", "content": "protected\n"})),
		pigtest.Text("done"),
	)
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext, approval}})
	if result.ExitCode != 0 {
		t.Fatalf("host exited %d: %s", result.ExitCode, result.Stderr)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 6 {
		t.Fatalf("expected six results, got %d", len(results))
	}
	if !strings.Contains(results[0], "Version: ") || !strings.Contains(results[1], anchor(1, "created")) || !strings.Contains(results[2], "Applied 1 edit") || !strings.Contains(results[3], "Version: ") || !strings.Contains(results[4], "stale version") {
		t.Fatalf("coordinated write/read/edit results: %q", results[:5])
	}
	if !strings.Contains(results[5], guard.Prefix+"blocked ") {
		t.Fatalf("replacement write bypassed guard: %q", results[5])
	}
	content, err := os.ReadFile(filepath.Join(home.Work, "new.txt"))
	if err != nil || string(content) != "explicit overwrite\n" {
		t.Fatalf("stale edit overwrote explicit write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home.Work, ".env")); !os.IsNotExist(err) {
		t.Fatal("blocked write created a protected file")
	}
}
