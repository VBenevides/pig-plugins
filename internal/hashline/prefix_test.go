package hashline

import "testing"

func TestAnchorLikeReplacementTextIsLiteral(t *testing.T) {
	for _, text := range []string{"12#abcd|literal source", "1#0000|pasted rendered line", " 12#abcd|indented"} {
		result, err := ApplyEdits(ParseFile("old\n"), []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "old"), "lines": []any{text}}})
		if err != nil || result.File.Serialize() != text+"\n" {
			t.Fatalf("prefix was stripped/rejected: %q %v", result.File.Serialize(), err)
		}
	}
}
