package agentprompts

import (
	"strings"
	"testing"
)

func TestDefaultPrompt(t *testing.T) {
	options := map[string]any{"appendSystemPrompt": "KEEP", "contextFiles": "KEEP CONTEXT"}
	changed, err := Apply(options)
	if err != nil || !changed || options["customPrompt"] != strings.TrimSpace(System) {
		t.Fatalf("changed=%v err=%v options=%v", changed, err, options)
	}
	if options["appendSystemPrompt"] != "KEEP" || options["contextFiles"] != "KEEP CONTEXT" {
		t.Fatal("external instructions changed")
	}
	if changed, err := Apply(options); changed || err != nil {
		t.Fatalf("not idempotent: %v %v", changed, err)
	}
}

func TestUserPromptIsPreserved(t *testing.T) {
	options := map[string]any{"customPrompt": "USER", "forceSystemPrompt": "USER with extension"}
	if changed, err := Apply(options); changed || err != nil || options["customPrompt"] != "USER" || options["forceSystemPrompt"] != "USER with extension" {
		t.Fatalf("user override changed: %v %v", options, err)
	}
}

func TestLateInitializationFails(t *testing.T) {
	options := map[string]any{"forceSystemPrompt": "EARLIER HANDLER"}
	if _, err := Apply(options); err == nil {
		t.Fatal("late initialization accepted")
	}
}
