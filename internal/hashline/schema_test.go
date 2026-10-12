package hashline

import (
	"regexp"
	"testing"
)

func TestToolSchemasExposeRuntimeBoundsAndAnchorSyntax(t *testing.T) {
	read := ReadSchema()
	edit := EditSchema()
	if read["additionalProperties"] != false || edit["additionalProperties"] != false || WriteSchema()["additionalProperties"] != false {
		t.Fatal("tool schema permits unknown fields")
	}
	properties := read["properties"].(map[string]any)
	for _, field := range []string{"offset", "limit"} {
		schema := properties[field].(map[string]any)
		if schema["type"] != "integer" || schema["minimum"] != 1 {
			t.Fatalf("%s schema does not require positive integers", field)
		}
	}
	edits := edit["properties"].(map[string]any)["edits"].(map[string]any)
	if edits["minItems"] != 1 || edits["maxItems"] != MaxEditOperations {
		t.Fatal("operation limits differ from runtime")
	}
	item := edits["items"].(map[string]any)
	if item["additionalProperties"] != false {
		t.Fatal("operation schema permits unknown fields")
	}
	fields := item["properties"].(map[string]any)
	for _, field := range []string{"anchor", "end"} {
		pattern := regexp.MustCompile(fields[field].(map[string]any)["pattern"].(string))
		for _, value := range []string{"1#abcd", "200#1234"} {
			if !pattern.MatchString(value) {
				t.Fatalf("schema rejects valid anchor %q", value)
			}
		}
		for _, value := range []string{"0#abcd", "1#ABCd", "1#ab", "1#abcd|source", "1#"} {
			if pattern.MatchString(value) {
				t.Fatalf("schema accepts malformed anchor %q", value)
			}
		}
	}
	lines := fields["lines"].(map[string]any)
	if lines["minItems"] != 1 || lines["maxItems"] != MaxEditLines {
		t.Fatal("replacement-line limits differ from runtime")
	}
}
