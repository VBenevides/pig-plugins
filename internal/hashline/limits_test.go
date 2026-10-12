package hashline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMutationBudgetsRejectBeforePreparation(t *testing.T) {
	cases := []struct {
		name, content string
		edits         []any
	}{
		{"operations", "one\n", make([]any, MaxEditOperations+1)},
		{"replacement-lines", "one\n", []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": make([]string, MaxEditLines+1)}}},
		{"replacement-bytes", "one\n", []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []string{strings.Repeat("x", MaxEditBytes+1)}}}},
		{"cumulative-lines", "one\ntwo\n", []any{
			map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": make([]string, MaxEditLines/2+1)},
			map[string]any{"op": "replace", "anchor": FormatAnchor(2, "two"), "lines": make([]string, MaxEditLines/2)},
		}},
		{"cumulative-bytes", "one\ntwo\n", []any{
			map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []string{strings.Repeat("x", MaxEditBytes/2+1)}},
			map[string]any{"op": "replace", "anchor": FormatAnchor(2, "two"), "lines": []string{strings.Repeat("x", MaxEditBytes/2)}},
		}},
		{"result-lines", strings.Repeat("one\n", MaxEditLines), []any{map[string]any{"op": "insert_before", "anchor": FormatAnchor(1, "one"), "lines": []string{"extra"}}}},
		{"result-bytes", "one\n", []any{map[string]any{"op": "insert_after", "anchor": FormatAnchor(1, "one"), "lines": []string{strings.Repeat("x", MaxEditBytes)}}}},
	}
	for i := range cases[0].edits {
		cases[0].edits[i] = map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []string{"updated"}}
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, item.content, 0644)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Edit(dir, map[string]any{"path": "f.txt", "version": ContentVersion([]byte(item.content)), "edits": item.edits})
			if err == nil || !strings.Contains(err.Error(), "exceed") {
				t.Fatalf("expected size-limit rejection, got %v", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != item.content || !os.SameFile(before, after) {
				t.Fatal("oversize rejection mutated the original")
			}
			if leftovers := tempFiles(t, dir); len(leftovers) != 0 {
				t.Fatal("oversize request reached temporary-file preparation")
			}
		})
	}
}

func TestExactMutationLimitsAreAccepted(t *testing.T) {
	file := ParseFile("one\n")
	// No final terminator keeps the exact byte boundary independent of EOLs.
	file.Lines[0].EOL = ""
	result, err := ApplyEdits(file, []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []string{strings.Repeat("x", MaxEditBytes)}}})
	if err != nil || len(result.File.Serialize()) != MaxEditBytes {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
	lines := make([]string, MaxEditLines)
	if result, err := ApplyEdits(ParseFile("one\n"), []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": lines}}); err != nil || len(result.File.Lines) != MaxEditLines {
		t.Fatalf("exact line limit rejected: %v", err)
	}
	var content strings.Builder
	operations := make([]any, MaxEditOperations)
	for i := range operations {
		text := fmt.Sprintf("line%d", i)
		fmt.Fprintln(&content, text)
		operations[i] = map[string]any{"op": "replace", "anchor": FormatAnchor(i+1, text), "lines": []string{"updated"}}
	}
	if _, err := ApplyEdits(ParseFile(content.String()), operations); err != nil {
		t.Fatalf("exact operation limit rejected: %v", err)
	}
}
