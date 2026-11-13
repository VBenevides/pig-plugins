package promptcheck

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The legacy file is the former SYSTEM.md, APPEND_SYSTEM.md and AGENTS.md joined by blank lines.
func TestLegacyInstructionsRemainExact(t *testing.T) {
	const want = "4ce3e1c96486aa687189125cb4be56be583ee37f471e8da7e431ee8a59e2e175"
	data, err := os.ReadFile(filepath.Join("..", "..", "prompts", "agent", "legacy", "SYSTEM.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
		t.Fatalf("legacy SYSTEM.md changed: %s", got)
	}
	current, err := os.ReadFile(files["SYSTEM.md"])
	if err != nil {
		t.Fatal(err)
	}
	if len(current)*2 >= len(data) {
		t.Fatalf("default bytes=%d legacy=%d; require at least 50%% source reduction", len(current), len(data))
	}
}

func TestDefaultKeepsEditAndSecurityContracts(t *testing.T) {
	system := load(t)["SYSTEM.md"]
	for _, phrase := range []string{
		"Version exactly", "Search results, outlines, truncated lines", "Read every affected range", "fully displayed source",
		"Put op in each operation", "Delete omits lines", "With native file tools, follow their schemas instead",
		"mutation is their documented purpose", "Do not silently overwrite", "preserve causes", "never log sensitive payloads",
		"use parameterized queries", "Do not disable TLS verification", "only for new files",
	} {
		if !strings.Contains(system, phrase) {
			t.Errorf("missing contract %q", phrase)
		}
	}
}
