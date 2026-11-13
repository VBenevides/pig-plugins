package promptcheck

import (
	"strings"
	"testing"
)

func TestDefaultKeepsEditAndSecurityContracts(t *testing.T) {
	system := load(t)["SYSTEM.md"]
	for _, phrase := range []string{
		"Version exactly", "Search results, outlines, truncated lines", "Read the affected source first",
		"Native edit uses exact oldText/newText", "Only when the active tools provide hashline anchors", "Use op and lines as the active edit schema requires",
		"mutation is their documented purpose", "Do not silently overwrite", "preserve causes", "never log sensitive payloads",
		"use parameterized queries", "Do not disable TLS verification", "only for new files",
	} {
		if !strings.Contains(system, phrase) {
			t.Errorf("missing contract %q", phrase)
		}
	}
}
