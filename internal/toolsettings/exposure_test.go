package toolsettings

import (
	"strings"
	"testing"
)

func TestSchemaTotalsExcludeUnloadedDeferredTools(t *testing.T) {
	m := &Menu{ShowHelp: true, Tabs: [2][]Row{{
		{Name: "core", Context: "1234", Enabled: true, Visible: true, Exposure: "direct"},
		{Name: "later", Context: "12345678", Enabled: true, Exposure: "deferred"},
	}, nil}}
	text := strings.Join(m.Render(96), "\n")
	for _, want := range []string{"Tools: 4 chars / ~1 tokens", "Discoverable: 1 tools / 8 schema chars", "defer", "direct"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	m.Tabs[0][1].Visible = true
	text = strings.Join(m.Render(96), "\n")
	for _, want := range []string{"Tools: 12 chars / ~3 tokens", "Discoverable: 0 tools / 0 schema chars", "loaded"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}
