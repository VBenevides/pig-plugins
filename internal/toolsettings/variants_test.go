package toolsettings

import (
	"strings"
	"testing"
)

func TestBothFileVariantsAreVisibleAndExclusive(t *testing.T) {
	m := &Menu{Tabs: [2][]Row{{{ID: "read", Name: "read", Enabled: true, DefaultEnabled: true, Context: "read schema", Visible: true}, {ID: "edit", Name: "edit", Enabled: true, DefaultEnabled: true}, {ID: "write", Name: "write", Enabled: false, DefaultEnabled: true}}, nil}}
	m.ExpandFileVariants()
	if len(m.Tabs[0]) != 6 {
		t.Fatal(m.Tabs[0])
	}
	text := strings.Join(m.Render(96), "\n")
	for _, name := range []string{"read (hashline)", "read (native)", "edit (hashline)", "edit (native)"} {
		if !strings.Contains(text, name) {
			t.Fatal(text)
		}
	}
	m.Cursor = 1
	m.Handle(" ")
	if !m.NativeFiles {
		t.Fatal("native choice did not change implementation")
	}
	for _, r := range m.Tabs[0] {
		if r.Enabled && r.Variant != "native" {
			t.Fatal("both implementations enabled")
		}
	}
	if !m.Tabs[0][1].Enabled || !m.Tabs[0][3].Enabled || m.Tabs[0][5].Enabled {
		t.Fatal("enable state was not transferred")
	}
	m.ApplyPreset(MinimalPreset)
	if !m.NativeFiles || m.presetLabel() != MinimalPreset {
		t.Fatal("native profile incorrect")
	}
	for _, r := range m.Tabs[0] {
		if r.Enabled != (r.Variant == "native") {
			t.Fatal(r)
		}
	}
	m.Cursor = 0
	m.Handle(" ")
	if m.NativeFiles {
		t.Fatal("hashline choice did not switch back")
	}
}
