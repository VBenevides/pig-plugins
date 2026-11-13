package toolsettings

import (
	"strings"
	"testing"
)

func TestDeferredHeaderTracksDiscoveryChoice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tools  []Row
		status string
	}{
		{"enabled", []Row{{ID: "tool_search", Enabled: true}}, "VISIBLE"},
		{"disabled", []Row{{ID: "tool_search", Enabled: false}}, "HIDDEN"},
		{"unavailable", nil, "HIDDEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			menu := &Menu{}
			menu.Tabs[0] = append(tc.tools, Row{ID: "todo", Group: "Deferred", Enabled: true})
			for _, wide := range []bool{false, true} {
				rendered := menu.renderEntry(entry{header: true, group: "Deferred"}, " ", 60, wide)
				if !strings.Contains(rendered, "Deferred - "+tc.status) {
					t.Fatalf("header=%s", rendered)
				}
			}
			if menu.Tabs[0][len(menu.Tabs[0])-1].Group != "Deferred" {
				t.Fatal("display changed group identity")
			}
		})
	}
	menu := &Menu{Tabs: [2][]Row{{{ID: "tool_search", Enabled: false}}}}
	menu.Tabs[0][0].Enabled = true
	if got := menu.renderEntry(entry{header: true, group: "Deferred"}, " ", 60, false); !strings.Contains(got, "VISIBLE") {
		t.Fatal("staged toggle did not update header")
	}
}
