package agentprompts

import (
	"strings"
	"testing"
)

func text(t *testing.T, style string) string {
	t.Helper()
	s, err := Content(style)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(s)
}

func TestBuiltInStylesDiffer(t *testing.T) {
	if text(t, "default") == "" || text(t, "default") == text(t, "legacy") {
		t.Fatal("styles are empty or identical")
	}
}

func TestSelectFillsAbsentCustomPromptAndIsIdempotent(t *testing.T) {
	for _, style := range Styles {
		options := map[string]any{"appendSystemPrompt": "KEEP APPEND", "contextFiles": []any{map[string]any{"path": "AGENTS.md", "content": "KEEP"}}}
		changed, err := Select(options, style)
		if err != nil || !changed || options["customPrompt"] != text(t, style) {
			t.Fatalf("%s: changed=%v err=%v", style, changed, err)
		}
		if options["appendSystemPrompt"] != "KEEP APPEND" || options["contextFiles"].([]any)[0].(map[string]any)["content"] != "KEEP" {
			t.Fatal("changed external instructions")
		}
		if changed, err = Select(options, style); err != nil || changed {
			t.Fatalf("repeat changed=%v err=%v", changed, err)
		}
	}
}

func TestSelectSwapsShippedPromptAndKeepsForcedSuffix(t *testing.T) {
	options := map[string]any{"customPrompt": text(t, "default"), "forceSystemPrompt": text(t, "default") + "\nKEEP EARLIER HANDLER"}
	changed, err := Select(options, "legacy")
	if err != nil || !changed || options["customPrompt"] != text(t, "legacy") {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if options["forceSystemPrompt"] != text(t, "legacy")+"\nKEEP EARLIER HANDLER" {
		t.Fatal("forced prompt not swapped")
	}
}

func TestUserSystemPromptIsNotReplaced(t *testing.T) {
	options := map[string]any{"customPrompt": "my custom instructions"}
	changed, err := Select(options, "legacy")
	if err != nil || changed || options["customPrompt"] != "my custom instructions" {
		t.Fatalf("options=%v err=%v", options, err)
	}
}

func TestSelectRejectsUnknownStyleAndAmbiguousForcedPrompt(t *testing.T) {
	if _, err := Select(map[string]any{}, "typo"); err == nil {
		t.Fatal("unknown style accepted")
	}
	source := text(t, "default")
	for _, forced := range []string{"unrelated", source + source} {
		options := map[string]any{"customPrompt": source, "forceSystemPrompt": forced}
		if _, err := Select(options, "legacy"); err == nil {
			t.Fatal("ambiguous replacement accepted")
		}
	}
	if _, err := Content("../default"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
