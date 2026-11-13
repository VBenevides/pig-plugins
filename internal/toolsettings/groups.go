package toolsettings

import (
	"path/filepath"
	"strings"
)

// entry is one visible line of the list: a category header (header == true) or the tool/skill at Tabs[tab][row].
type entry struct {
	header bool
	group  string
	row    int
}

var builtinGroups = map[string]string{
	"read": "Files", "edit": "Files", "write": "Files", "ls": "Files", "find": "Files", "grep": "Files",
	"bash": "Shell", "powershell": "Shell",
}

// GroupFor picks a category for a tool. Built-in tools use a fixed table, MCP tools group by server, and
// extension tools group by the extension directory or file that provides them.
func GroupFor(name, source string) string {
	if g, ok := builtinGroups[name]; ok {
		return g
	}
	// web_search reaches its Parallel and Exa backends through their MCP tools, so they belong together.
	if name == "web_search" || name == "url_context" || strings.HasPrefix(name, "mcp__parallel__") || strings.HasPrefix(name, "mcp__exa__") {
		return "Web & Search"
	}
	if strings.HasPrefix(source, "builtin:") {
		return "Tool discovery"
	}
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		if server, _, found := strings.Cut(rest, "__"); found && server != "" {
			return "MCP: " + server
		}
		return "MCP"
	}
	switch source {
	case "", "builtin", "unknown source":
		return "Core"
	}
	if !strings.ContainsAny(source, `/\`) {
		return source
	}
	dir, file := filepath.Split(filepath.ToSlash(source))
	base := strings.TrimSuffix(file, filepath.Ext(file))
	switch base {
	case "index", "main", "extension", "":
		if parent := filepath.Base(filepath.Clean(dir)); parent != "." && parent != "/" {
			return parent
		}
		return "Extensions"
	}
	return base
}

// CollapseGroups folds every category except keep, so a long tool list opens as a short overview.
func (m *Menu) CollapseGroups(keep string) {
	m.Collapsed = map[string]bool{}
	for _, r := range m.Tabs[0] {
		if r.Group != "" && r.Group != keep {
			m.Collapsed[r.Group] = true
		}
	}
}

// entries flattens the active tab: grouped rows are gathered under a header in first-appearance order and
// hidden when collapsed; rows without a group stay in place with no header.
func (m *Menu) entries() []entry {
	rows := m.Tabs[m.Tab]
	var out []entry
	seen := map[string]bool{}
	for i, r := range rows {
		if r.Group == "" {
			out = append(out, entry{row: i})
			continue
		}
		if seen[r.Group] {
			continue
		}
		seen[r.Group] = true
		out = append(out, entry{header: true, group: r.Group, row: i})
		if m.Collapsed[r.Group] {
			continue
		}
		for j := i; j < len(rows); j++ {
			if rows[j].Group == r.Group {
				out = append(out, entry{group: r.Group, row: j})
			}
		}
	}
	return out
}

// groupState counts the selectable rows of a group and how many are enabled. Inactive file-tool variants are
// excluded: they cannot be enabled until selected.
func (m *Menu) groupState(group string) (enabled, total int) {
	for _, r := range m.Tabs[m.Tab] {
		if r.Group != group || (m.Tab == 0 && !m.SelectedVariant(r)) {
			continue
		}
		total++
		if r.Enabled {
			enabled++
		}
	}
	return enabled, total
}

// toggleGroup enables every selectable row, or disables them all when they already are enabled.
func (m *Menu) toggleGroup(group string) {
	enabled, total := m.groupState(group)
	want := enabled < total
	for i := range m.Tabs[m.Tab] {
		r := &m.Tabs[m.Tab][i]
		if r.Group == group && (m.Tab != 0 || m.SelectedVariant(*r)) {
			r.Enabled = want
		}
	}
}

// groupChars sums the discovery context of enabled, visible rows in a group.
func (m *Menu) groupChars(group string) int {
	chars := 0
	for _, r := range m.Tabs[m.Tab] {
		if r.Group == group && r.Enabled && r.Visible {
			n, _ := Characters(r.Context)
			chars += n
		}
	}
	return chars
}

// fold collapses or expands the group of the entry under the cursor. After collapsing, the cursor moves to
// the group header so it never points at a hidden row.
func (m *Menu) fold(collapse bool) {
	es := m.entries()
	if m.Cursor >= len(es) {
		return
	}
	group := es[m.Cursor].group
	if group == "" {
		return
	}
	if m.Collapsed == nil {
		m.Collapsed = map[string]bool{}
	}
	m.Collapsed[group] = collapse
	if !collapse {
		return
	}
	for i, e := range m.entries() {
		if e.header && e.group == group {
			m.Cursor = i
		}
	}
}

func (m *Menu) hasGroups() bool {
	for _, r := range m.Tabs[m.Tab] {
		if r.Group != "" {
			return true
		}
	}
	return false
}
