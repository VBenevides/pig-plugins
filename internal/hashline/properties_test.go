package hashline

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzParseSerializeRoundTrip(f *testing.F) {
	for _, content := range []string{"", "\ufeff", "one\r\ntwo\nthree\r", "😀\n日本語", "\xff\x00\r\n", "\ufeff\ufeff\n"} {
		f.Add(content)
	}
	f.Fuzz(func(t *testing.T, content string) {
		file := ParseFile(content)
		if got := file.Serialize(); got != content {
			t.Fatalf("parse/serialize changed bytes: %q -> %q", content, got)
		}
		if reparsed := ParseFile(file.Serialize()); !reflect.DeepEqual(file, reparsed) {
			t.Fatal("reparsing changed line structure")
		}
	})
}

func FuzzBatchEditsAreDeterministicAndDoNotModifyOriginal(f *testing.F) {
	for _, seed := range []struct {
		content, replacement, anchor string
		conflict                     bool
	}{
		{"one\ntwo\n", "changed", "", false},
		{"\ufeffone\r\ntwo\n", "😀", "", true},
		{"one", "new", "0#abcd", false},
		{"one\n", "\x00", "", false},
		{"one\r\ntwo", "new\nline", "1#zzzz", true},
	} {
		f.Add(seed.content, seed.replacement, seed.anchor, seed.conflict)
	}
	f.Fuzz(func(t *testing.T, content, replacement, anchor string, conflict bool) {
		// Bound resource consumption per fuzz input; ordinary budget regressions cover larger requests.
		if len(content) > 64*1024 || len(replacement) > 64*1024 {
			return
		}
		file := ParseFile(content)
		original := file.Serialize()
		before := append([]Line(nil), file.Lines...)
		if anchor == "" && len(file.Lines) > 0 {
			anchor = FormatAnchor(1, file.Lines[0].Text)
		}
		ops := []any{map[string]any{"op": "replace", "anchor": anchor, "lines": []any{replacement}}}
		if conflict {
			ops = append(ops, map[string]any{"op": "delete", "anchor": anchor})
		}
		first, err := ApplyEdits(file, ops)
		second, secondErr := ApplyEdits(file, ops)
		if (err == nil) != (secondErr == nil) || (err != nil && err.Error() != secondErr.Error()) || !reflect.DeepEqual(first, second) {
			t.Fatal("same batch produced different results")
		}
		if file.Serialize() != original || !reflect.DeepEqual(file.Lines, before) {
			t.Fatal("batch modified its original snapshot")
		}
		if conflict && err == nil {
			t.Fatal("conflicting batch was accepted")
		}
		if err == nil {
			if !utf8.ValidString(replacement) || strings.ContainsAny(replacement, "\x00\r\n") {
				t.Fatal("invalid replacement was accepted")
			}
			resolvedAnchor, anchorErr := parseAnchor(anchor, "anchor")
			if anchorErr != nil || resolvedAnchor.line < 1 || resolvedAnchor.line > len(first.File.Lines) || first.File.Lines[resolvedAnchor.line-1].Text != replacement {
				t.Fatal("valid replacement did not appear in the result")
			}
		}
	})
}
