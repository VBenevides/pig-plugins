package hashlineedit_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// Boundaries through the real extension: a capped range names its continuation,
// destroying a line the capped range never displayed is refused, and a batch
// over the operation budget is refused. The file stays untouched throughout.
func TestPayloadAndOutputBoundariesThroughRealExtension(t *testing.T) {
	pigtest.RequirePig(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	var content strings.Builder
	for i := 1; i <= 2500; i++ {
		fmt.Fprintf(&content, "row %d\n", i)
	}
	big := filepath.Join(home.Work, "big.txt")
	if err := os.WriteFile(big, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	version := hashline.ContentVersion([]byte(content.String()))
	unseen := map[string]any{"path": "big.txt", "version": version, "edits": []any{
		map[string]any{"op": "delete", "anchor": anchor(2300, "row 2300")}}}
	tooMany := make([]any, 0, hashline.MaxEditOperations+1)
	for i := 1; i <= hashline.MaxEditOperations+1; i++ {
		tooMany = append(tooMany, map[string]any{"op": "insert_after", "anchor": anchor(i, fmt.Sprintf("row %d", i)), "lines": []any{"x"}})
	}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "big.txt", "offset": 1, "limit": 2500})),
		pigtest.Calls(pigtest.Call("edit", unseen)),
		pigtest.Calls(pigtest.Call("edit", map[string]any{"path": "big.txt", "version": version, "edits": tooMany})),
		pigtest.Text("done"),
	)
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}})
	if result.ExitCode != 0 {
		t.Fatalf("pig exited %d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 3 {
		t.Fatalf("got %d tool results", len(results))
	}
	if !strings.Contains(results[0], "output cap reached. Continue with offset=2001.") {
		t.Errorf("capped read lacks continuation: %q", results[0][max(0, len(results[0])-200):])
	}
	if !strings.Contains(results[1], "were not fully displayed") {
		t.Errorf("unseen deletion was not refused: %q", results[1])
	}
	// The host schema (maxItems) normally refuses first; the runtime budget is
	// the second line of defense and is covered by the core API tests.
	if !strings.Contains(results[2], "256") && !strings.Contains(results[2], "exceeds") {
		t.Errorf("oversized batch was not refused: %.200q", results[2])
	}
	if got, _ := os.ReadFile(big); string(got) != content.String() {
		t.Error("boundary rejections changed the file")
	}
}
