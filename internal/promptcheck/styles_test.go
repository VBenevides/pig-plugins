package promptcheck

import (
	"strings"
	"testing"
)

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
