package hashlineedit_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func anchor(n int, text string) string { return hashline.FormatAnchor(n, text) }

// The extension must replace the built-in read and edit in a real pig session: one tool of each name, anchored read
// output, a strict edit that applies, and a rejected stale edit that leaves the file alone.
func TestReadAndEditReplaceTheBuiltIns(t *testing.T) {
	pigtest.RequirePig(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	notes := filepath.Join(home.Work, "notes.txt")
	if err := os.WriteFile(notes, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}
	if err := os.WriteFile(filepath.Join(home.Work, "dot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	replaceBeta := map[string]any{"path": "notes.txt", "edits": []any{
		map[string]any{"op": "replace", "anchor": anchor(2, "beta"), "lines": []any{"BETA"}}}}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "notes.txt"})),
		pigtest.Calls(pigtest.Call("edit", replaceBeta)),
		pigtest.Calls(pigtest.Call("edit", replaceBeta)), // stale: line 2 is now BETA
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "dot.png"})),
		pigtest.Text("done"),
	)
	defer mock.Close()

	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}})
	if result.ExitCode != 0 {
		t.Fatalf("pig exited %d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	names := pigtest.ToolNames(mock)
	if !reflect.DeepEqual(names, []string{"read", "bash", "edit", "write"}) {
		t.Fatalf("tool names = %v, want [read bash edit write] (one read, one edit)", names)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 4 {
		t.Fatalf("got %d tool results: %q", len(results), results)
	}
	wantRead := strings.Join([]string{
		anchor(1, "alpha") + "|alpha", anchor(2, "beta") + "|beta", anchor(3, "gamma") + "|gamma"}, "\n")
	if results[0] != wantRead {
		t.Errorf("read result\nwant: %q\n got: %q", wantRead, results[0])
	}
	if !strings.HasPrefix(results[1], "Applied 1 edit(s) to "+notes) || !strings.Contains(results[1], anchor(2, "BETA")+"|BETA") {
		t.Errorf("edit result: %q", results[1])
	}
	if !strings.Contains(results[2], "edit rejected") || !strings.Contains(results[2], "stale anchor") {
		t.Errorf("stale edit result: %q", results[2])
	}
	if !strings.Contains(results[3], "Read image file [image/png]") {
		t.Errorf("image result: %q", results[3])
	}
	if got, _ := os.ReadFile(notes); string(got) != "alpha\nBETA\ngamma\n" {
		t.Errorf("file content %q", got)
	}
}
