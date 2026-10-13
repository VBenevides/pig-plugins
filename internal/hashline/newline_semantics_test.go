package hashline

import (
	"os"
	"path/filepath"
	"testing"
)

// Expectations are written by hand from the documented rules: a BOM stays,
// untouched lines keep their own terminator, new lines use the file's first
// terminator, and a missing final newline stays missing.
func TestByteOrderAndNewlineSemanticsAcrossOperations(t *testing.T) {
	type edit struct {
		op, anchorText string
		line           int
		lines          []any
		want           string
	}
	fixtures := []struct {
		name, content string
		edits         []edit
	}{
		{"bom-lf", "\ufeffa\nb\nc\n", []edit{
			{"replace", "b", 2, []any{"X"}, "\ufeffa\nX\nc\n"},
			{"delete", "c", 3, nil, "\ufeffa\nb\n"},
			{"insert_after", "c", 3, []any{"N"}, "\ufeffa\nb\nc\nN\n"},
			{"insert_before", "a", 1, []any{"N"}, "\ufeffN\na\nb\nc\n"}}},
		{"crlf", "a\r\nb\r\nc\r\n", []edit{
			{"replace", "b", 2, []any{"X"}, "a\r\nX\r\nc\r\n"},
			{"delete", "c", 3, nil, "a\r\nb\r\n"},
			{"insert_after", "c", 3, []any{"N"}, "a\r\nb\r\nc\r\nN\r\n"},
			{"insert_before", "a", 1, []any{"N"}, "N\r\na\r\nb\r\nc\r\n"}}},
		{"mixed-first-lf", "a\nb\r\nc\n", []edit{
			{"replace", "b", 2, []any{"X"}, "a\nX\nc\n"},
			{"delete", "c", 3, nil, "a\nb\n"},
			{"insert_after", "c", 3, []any{"N"}, "a\nb\r\nc\nN\n"},
			{"insert_before", "a", 1, []any{"N"}, "N\na\nb\r\nc\n"}}},
		{"no-final-newline", "a\nb", []edit{
			{"replace", "b", 2, []any{"X"}, "a\nX"},
			{"delete", "b", 2, nil, "a"},
			{"insert_after", "b", 2, []any{"N"}, "a\nb\nN"},
			{"insert_before", "a", 1, []any{"N"}, "N\na\nb"}}},
		{"bom-crlf-no-final-newline", "\ufeffa\r\nb", []edit{
			{"replace", "b", 2, []any{"X"}, "\ufeffa\r\nX"},
			{"delete", "b", 2, nil, "\ufeffa"},
			{"insert_after", "b", 2, []any{"N"}, "\ufeffa\r\nb\r\nN"},
			{"insert_before", "a", 1, []any{"N"}, "\ufeffN\r\na\r\nb"}}},
	}
	for _, fixture := range fixtures {
		for _, item := range fixture.edits {
			t.Run(fixture.name+"/"+item.op, func(t *testing.T) {
				dir := realTempDir(t)
				path := filepath.Join(dir, "f.txt")
				writeFile(t, path, fixture.content, 0600)
				operation := map[string]any{"op": item.op, "anchor": FormatAnchor(item.line, item.anchorText)}
				if item.lines != nil {
					operation["lines"] = item.lines
				}
				_, err := Edit(dir, map[string]any{"path": "f.txt", "version": ContentVersion([]byte(fixture.content)), "edits": []any{operation}})
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != item.want {
					t.Fatalf("got %q, want %q (%v)", got, item.want, err)
				}
			})
		}
	}
}
