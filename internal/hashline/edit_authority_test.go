package hashline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEditResponseSupportsChainedEdits(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\ntwo\nthree\nfour\nfive\n", 0644)
	engine := NewEngine()
	ctx := context.Background()
	read, err := engine.ReadContext(ctx, dir, map[string]any{"path": "f", "offset": 1, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.EditContext(ctx, dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
		map[string]any{"op": "insert_before", "anchor": FormatAnchor(1, "one"), "lines": []any{"zero"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		line          int
		before, after string
	}{{2, "one", "ONE"}, {3, "two", "TWO"}} {
		if !strings.Contains(result.Text, FormatLine(step.line, step.before)) {
			t.Fatalf("response did not display next anchor: %s", result.Text)
		}
		result, err = engine.EditContext(ctx, dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{
			map[string]any{"op": "replace", "anchor": FormatAnchor(step.line, step.before), "lines": []any{step.after}},
		}})
		if err != nil {
			t.Fatalf("chained edit: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "zero\nONE\nTWO\nthree\nfour\nfive\n" {
		t.Fatalf("content: %q %v", data, err)
	}
	if result.Details["version"] != ContentVersion(data) {
		t.Fatal("response has stale version")
	}
	if _, err := engine.EditContext(ctx, dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
		map[string]any{"op": "delete", "anchor": FormatAnchor(1, "zero")},
	}}); ErrorCategoryOf(err) != CategoryStale {
		t.Fatalf("original version authorized an edit: %v", err)
	}
}

func TestEditResponseDropsCoverageFromPreviousVersion(t *testing.T) {
	dir := realTempDir(t)
	var lines []string
	for n := 1; n <= 100; n++ {
		lines = append(lines, fmt.Sprintf("line%d", n))
	}
	writeFile(t, filepath.Join(dir, "f"), strings.Join(lines, "\n")+"\n", 0644)
	engine := NewEngine()
	read := engineRead(t, engine, dir, "f")
	result, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "line1"), "lines": []any{"changed"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Details["fullyDisplayedRanges"]; !reflect.DeepEqual(got, [][2]int{{1, 3}}) {
		t.Fatalf("coverage: %v", got)
	}
	for _, op := range []string{"replace", "delete"} {
		edit := map[string]any{"op": op, "anchor": FormatAnchor(90, "line90")}
		if op == "replace" {
			edit["lines"] = []any{"hidden"}
		}
		if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{edit}}); err == nil || !strings.Contains(err.Error(), "not fully displayed") {
			t.Fatalf("previous coverage survived: %v", err)
		}
	}
	// A fresh range read of this version still merges with the edit response.
	if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": 90, "limit": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{
		map[string]any{"op": "delete", "anchor": FormatAnchor(90, "line90")},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestEditResponseCoverageExcludesTruncatedSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lines  []any
		want   [][2]int
		hidden int
	}{
		{"shortened", []any{strings.Repeat("x", MaxLineChars+1)}, [][2]int{}, 1},
		{"shortened-unicode", []any{strings.Repeat("😀", MaxLineChars/2+1)}, [][2]int{}, 1},
		{"line-cap", repeatEditLines("short", resultMaxLines+10), [][2]int{{1, resultMaxLines}}, resultMaxLines + 1},
		{"byte-cap", repeatEditLines(strings.Repeat("界", MaxLineChars), 80), [][2]int{{1, 8}}, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := realTempDir(t)
			writeFile(t, filepath.Join(dir, "f"), "one\n", 0644)
			engine := NewEngine()
			read := engineRead(t, engine, dir, "f")
			result, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": tc.lines},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Details["fullyDisplayedRanges"]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("coverage: %v want %v", got, tc.want)
			}
			if len(result.Text) > MaxRangeBytes {
				t.Fatal("response exceeds byte limit")
			}
			if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{
				map[string]any{"op": "delete", "anchor": FormatAnchor(tc.hidden, tc.lines[tc.hidden-1].(string))},
			}}); err == nil || !strings.Contains(err.Error(), "not fully displayed") {
				t.Fatalf("truncated source authorized deletion: %v", err)
			}
		})
	}
}

func repeatEditLines(text string, count int) []any {
	lines := make([]any, count)
	for i := range lines {
		lines[i] = text
	}
	return lines
}

func TestEditResponseCoverageDoesNotBridgeSummaryGaps(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, "f"), strings.Repeat("line\n", 100), 0644)
	engine := NewEngine()
	read := engineRead(t, engine, dir, "f")
	result, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "line"), "lines": []any{"first"}},
		map[string]any{"op": "replace", "anchor": FormatAnchor(100, "line"), "lines": []any{"last"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Details["fullyDisplayedRanges"]; !reflect.DeepEqual(got, [][2]int{{1, 3}, {98, 100}}) {
		t.Fatalf("coverage: %v", got)
	}
	if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{
		map[string]any{"op": "delete", "anchor": FormatAnchor(3, "line"), "end": FormatAnchor(98, "line")},
	}}); err == nil || !strings.Contains(err.Error(), "not fully displayed") {
		t.Fatalf("summary gap authorized a destructive range: %v", err)
	}
}

func TestRenewedEditAuthorityRemainsBounded(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, "f"), "one\n", 0644)
	engine := newEngine(2, 128)
	read := engineRead(t, engine, dir, "f")
	result, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{strings.Repeat("x", 200)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(engine.reads) != 0 || engine.bytes != 0 {
		t.Fatal("oversized edit snapshot exceeded retention budget")
	}
	if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{
		map[string]any{"op": "delete", "anchor": FormatAnchor(1, strings.Repeat("x", 200))},
	}}); ErrorCategoryOf(err) != CategoryStale {
		t.Fatalf("unretained response authorized an edit: %v", err)
	}
}

func TestRenewedEditAuthorityStillRejectsRevokedOrChangedState(t *testing.T) {
	for _, change := range []string{"lifecycle", "session", "external-write", "write", "eviction"} {
		t.Run(change, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f")
			writeFile(t, path, "one\n", 0644)
			engine := newEngine(1, 4096)
			ctx := WithScope(context.Background(), "first")
			read, err := engine.ReadContext(ctx, dir, map[string]any{"path": "f"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.EditContext(ctx, dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"changed"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "lifecycle":
				engine.Invalidate()
			case "session":
				ctx = WithScope(context.Background(), "second")
			case "external-write":
				writeFile(t, path, "external\n", 0644)
			case "write":
				if _, err := engine.WriteContext(ctx, dir, map[string]any{"path": "f", "content": "written\n"}); err != nil {
					t.Fatal(err)
				}
			case "eviction":
				writeFile(t, filepath.Join(dir, "other"), "other\n", 0644)
				if _, err := engine.ReadContext(ctx, dir, map[string]any{"path": "other"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := engine.EditContext(ctx, dir, map[string]any{"path": "f", "version": result.Details["version"], "edits": []any{
				map[string]any{"op": "delete", "anchor": FormatAnchor(1, "changed")},
			}}); ErrorCategoryOf(err) != CategoryStale {
				t.Fatalf("invalid authority accepted: %v", err)
			}
		})
	}
}
