package hashline

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredErrorCategoriesSurviveToolWrapping(t *testing.T) {
	for _, test := range []struct {
		name     string
		category ErrorCategory
	}{
		{"invalid", CategoryInvalid}, {"stale", CategoryStale}, {"conflict", CategoryConflict}, {"permission", CategoryPermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f")
			writeFile(t, path, "one\n", 0644)
			params := map[string]any{"path": "f", "version": ContentVersion([]byte("one\n")), "edits": []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one")}}}
			switch test.name {
			case "invalid":
				params["edits"] = []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one")}}
			case "stale":
				params["version"] = ContentVersion([]byte("old\n"))
			case "conflict":
				params["edits"] = []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one")}, map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one")}}
			case "permission":
				if err := os.Chmod(path, 0444); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Edit(dir, params)
			if err == nil || ErrorCategoryOf(err) != test.category || !strings.Contains(err.Error(), "edit rejected") {
				t.Fatalf("category = %s, want %s: %v", ErrorCategoryOf(err), test.category, err)
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil || string(content) != "one\n" {
				t.Fatal("rejection changed file")
			}
		})
	}
	for _, test := range []struct {
		err      error
		category ErrorCategory
	}{
		{fmt.Errorf("wrapped: %w", context.Canceled), CategoryCancelled},
		{fmt.Errorf("wrapped: %w", fs.ErrPermission), CategoryPermission},
		{&CommittedError{Path: "f", Err: context.Canceled}, CategoryCommitted},
	} {
		if got := ErrorCategoryOf(test.err); got != test.category {
			t.Fatalf("wrapped category = %s, want %s", got, test.category)
		}
	}
}

func TestConcurrentWriteHasDistinctErrorCategory(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\n", 0644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	err = ReplaceAtomically(snapshot, "new\n", func() { writeFile(t, path, "external\n", 0644) })
	if ErrorCategoryOf(err) != CategoryConcurrent {
		t.Fatalf("concurrent write category: %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "external\n" {
		t.Fatal("concurrent writer was overwritten")
	}
}
