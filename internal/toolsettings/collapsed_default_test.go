package toolsettings

import "testing"

func TestCollapseAllGroups(t *testing.T) {
	menu := &Menu{Tabs: [2][]Row{{
		{ID: "read", Group: "Files"},
		{ID: "bash", Group: "Shell"},
		{ID: "todo", Group: "Deferred"},
		{ID: "tool_search", Group: "Tool discovery"},
	}}}
	menu.CollapseGroups("")
	entries := menu.entries()
	if len(entries) != 4 {
		t.Fatalf("entries=%d; expected only headers", len(entries))
	}
	for _, entry := range entries {
		if !entry.header || !menu.Collapsed[entry.group] {
			t.Fatalf("section is expanded: %+v", entry)
		}
	}
	menu.fold(false)
	if menu.Collapsed["Files"] {
		t.Fatal("manual expansion failed")
	}
}
