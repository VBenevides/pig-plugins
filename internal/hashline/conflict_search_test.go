package hashline

import (
	"fmt"
	"testing"
)

func TestInsertionConflictSearchMatchesIndependentGapModel(t *testing.T) {
	file := ParseFile("one\ntwo\nthree\nfour\nfive\nsix\nseven\n")
	for gap := 0; gap <= len(file.Lines); gap++ {
		t.Run(fmt.Sprint(gap), func(t *testing.T) {
			kind := "insert_before"
			line := gap + 1
			if gap == len(file.Lines) {
				kind = "insert_after"
				line = gap
			}
			ops := []any{
				map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one"), "end": FormatAnchor(3, "three")},
				map[string]any{"op": "delete", "anchor": FormatAnchor(5, "five"), "end": FormatAnchor(6, "six")},
				map[string]any{"op": kind, "anchor": FormatAnchor(line, file.Lines[line-1].Text), "lines": []any{"inserted"}},
			}
			_, err := ApplyEdits(file, ops)
			conflict := gap == 1 || gap == 2 || gap == 5
			if (err != nil) != conflict {
				t.Fatalf("gap %d: conflict=%v, error=%v", gap, conflict, err)
			}
			if conflict && ErrorCategoryOf(err) != CategoryConflict {
				t.Fatalf("wrong conflict category: %v", err)
			}
			if file.Serialize() != "one\ntwo\nthree\nfour\nfive\nsix\nseven\n" {
				t.Fatal("conflict checking changed original")
			}
		})
	}
}
