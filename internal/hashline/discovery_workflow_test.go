package hashline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Discovery tools may print source locations or even content-derived anchors.
// Neither those strings nor a known digest substitutes for an Engine read.
func TestDiscoveryRequiresReadBeforeEdit(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "example.go")
	original := "package example\n\nfunc Example() {}\n"
	writeFile(t, path, original, 0644)
	engine := NewEngine()
	ctx := WithScope(context.Background(), "discovery-workflow")
	params := map[string]any{"path": "example.go", "version": ContentVersion([]byte(original)), "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(3, "func Example() {}"), "lines": []any{"func Example() { println(1) }"}},
	}}
	if _, err := engine.EditContext(ctx, dir, params); err == nil {
		t.Fatal("discovery anchor and digest authorized an unread file")
	}
	read, err := engine.ReadContext(ctx, dir, map[string]any{"path": "example.go", "offset": 3, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.Text, FormatAnchor(3, "func Example() {}")+"|func Example() {}") || read.Details["version"] != params["version"] {
		t.Fatalf("read did not return the current edit-ready anchor and version: %+v", read)
	}
	params["version"] = read.Details["version"]
	if _, err := engine.EditContext(ctx, dir, params); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package example\n\nfunc Example() { println(1) }\n" {
		t.Fatalf("file = %q", got)
	}
	// A successful mutation revokes authority even if the new digest is known.
	params["version"] = ContentVersion(got)
	params["edits"] = []any{map[string]any{"op": "delete", "anchor": FormatAnchor(3, "func Example() { println(1) }")}}
	if _, err := engine.EditContext(ctx, dir, params); err == nil {
		t.Fatal("edit summary authorized a subsequent edit without rereading")
	}
}
