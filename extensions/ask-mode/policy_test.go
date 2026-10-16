package askmodeext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestAskRejectsShellBeforeDispatch(t *testing.T) {
	output := filepath.Join(t.TempDir(), "must-not-exist")
	for _, command := range []string{"sort -o" + output + " input", "git diff --ext-diff", "ls -la"} {
		// These policy paths must not access the UI or execute the command.
		reason := check(sdk.Context{}, "bash", map[string]any{"command": command})
		if !strings.Contains(reason, "bash blocked") {
			t.Fatalf("%q: missing denial: %q", command, reason)
		}
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("unexpected shell output: %v", err)
	}
	for _, name := range []string{"read", "grep", "find", "ls"} {
		if reason := check(sdk.Context{}, name, nil); reason != "" {
			t.Errorf("dedicated %s blocked: %s", name, reason)
		}
	}
}

func TestAskExactToolPolicy(t *testing.T) {
	for _, name := range []string{
		"lsp_diagnostics", "lsp_hover", "lsp_definition", "lsp_references",
		"lsp_symbols", "lsp_rename_preview", "code_overview", "code_search",
		"memory_search", "memory_read", "tool_search",
	} {
		if reason := check(sdk.Context{}, name, nil); reason != "" {
			t.Errorf("reviewed tool %s rejected: %s", name, reason)
		}
	}
	// Synthetic discovered tools must remain blocked regardless of namespace.
	for _, name := range []string{
		"lsp_apply_edit", "lsp_rename_apply", "lsp_execute_command",
		"code_execute", "code_search_delete", "memory_delete", "memory_write",
		"memory_search_extra", "mcp_deploy", "unknown", "",
	} {
		for _, input := range []map[string]any{nil, {"readOnlyHint": true}} {
			if reason := check(sdk.Context{}, name, input); reason == "" {
				t.Errorf("unknown tool %q admitted with %v", name, input)
			}
		}
	}
}
