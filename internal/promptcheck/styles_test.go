package promptcheck

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyInstructionsRemainExact(t *testing.T) {
	hashes := map[string]string{
		"SYSTEM.md":        "ebf45e8909fa597a681479700c09325d4a1569c458f3511df4d2fb217040210f",
		"APPEND_SYSTEM.md": "74e5731d6c87de290cf5e13cafdec0ed24491d35d9b9e159c3b5881c6f64ee28",
		"AGENTS.md":        "2ef62a6146185a28a3acb2e3ba55f0d50fe4b60636813af4c2909c3e5e851d70",
	}
	legacy, current := 0, 0
	for name, want := range hashes {
		data, err := os.ReadFile(filepath.Join("..", "..", "prompts", "agent", "legacy", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Fatalf("legacy %s changed: %s", name, got)
		}
		legacy += len(data)
		data, err = os.ReadFile(files[name])
		if err != nil {
			t.Fatal(err)
		}
		current += len(data)
	}
	if current*2 >= legacy {
		t.Fatalf("default bytes=%d legacy=%d; require at least 50%% source reduction", current, legacy)
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
