package agentprompts

import (
	"path/filepath"
	"strings"
	"testing"
)

func text(t *testing.T, style, name string) string {
	t.Helper()
	s, err := Content(style, name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(s)
}

func TestSelectBothStylesAndPreserveExternalInstructions(t *testing.T) {
	for _, style := range []string{"default", "legacy"} {
		source := "default"
		if style == source {
			source = "legacy"
		}
		t.Run(style, func(t *testing.T) {
			agent := t.TempDir()
			system, appendix, rules := text(t, source, "SYSTEM.md"), text(t, source, "APPEND_SYSTEM.md"), text(t, source, "AGENTS.md")
			project := map[string]any{"path": filepath.Join(t.TempDir(), "AGENTS.md"), "content": rules}
			global := map[string]any{"path": filepath.Join(agent, "AGENTS.md"), "content": rules}
			options := map[string]any{"customPrompt": system, "appendSystemPrompt": appendix + "\n\nKEEP APPEND", "contextFiles": []any{global, project}, "forceSystemPrompt": system + "\n" + appendix + "\n" + rules + "\nKEEP EARLIER HANDLER"}
			changed, err := Select(options, style, agent)
			if err != nil || !changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			if options["customPrompt"] != text(t, style, "SYSTEM.md") || global["content"] != text(t, style, "AGENTS.md") || project["content"] != rules {
				t.Fatal("wrong ownership or style")
			}
			forced := options["forceSystemPrompt"].(string)
			if !strings.HasSuffix(forced, "KEEP EARLIER HANDLER") || !strings.HasSuffix(options["appendSystemPrompt"].(string), "KEEP APPEND") {
				t.Fatal("lost external instructions")
			}
			changed, err = Select(options, style, agent)
			if err != nil || changed {
				t.Fatalf("repeat selection=%v err=%v", changed, err)
			}
		})
	}
}

func TestCustomInstructionsAreNotReplaced(t *testing.T) {
	options := map[string]any{"customPrompt": "my custom instructions", "appendSystemPrompt": "custom append"}
	changed, err := Select(options, "legacy", t.TempDir())
	if err != nil || changed || options["customPrompt"] != "my custom instructions" {
		t.Fatalf("options=%v err=%v", options, err)
	}
}

func TestSelectRejectsUnknownStyleAndAmbiguousForcedPrompt(t *testing.T) {
	if _, err := Select(nil, "typo", t.TempDir()); err == nil {
		t.Fatal("unknown style accepted")
	}
	source := text(t, "default", "SYSTEM.md")
	for _, forced := range []string{"unrelated", source + source} {
		options := map[string]any{"customPrompt": source, "forceSystemPrompt": forced}
		if _, err := Select(options, "legacy", t.TempDir()); err == nil {
			t.Fatal("ambiguous replacement accepted")
		}
	}
	if _, err := Content("default", "../SYSTEM.md"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
