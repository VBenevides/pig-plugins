package toolsettings

import (
	"fmt"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
)

type Row struct {
	Name, ID, Source, Context string
	Enabled, Visible          bool
	LoadedChars               int
}

// Menu keeps edits local until the user saves. Reset requires an explicit second key.
type Menu struct {
	Tabs         [2][]Row
	Tab, Cursor  int
	ResetPending bool
	Reset        bool
	LoadedKnown  bool
}

func (m *Menu) Handle(key string) (done, save bool) {
	if m.ResetPending {
		switch key {
		case "y", "Y":
			m.Reset = true
			return true, true
		case "n", "N", "escape":
			m.ResetPending = false
		}
		return false, false
	}
	switch key {
	case "escape":
		return true, false
	case "enter":
		return true, true
	case "tab", "left", "right":
		m.Tab = 1 - m.Tab
		m.Cursor = 0
	case "up":
		m.Cursor = max(0, m.Cursor-1)
	case "down":
		m.Cursor = min(max(0, len(m.Tabs[m.Tab])-1), m.Cursor+1)
	case " ", "space":
		if len(m.Tabs[m.Tab]) > 0 {
			r := &m.Tabs[m.Tab][m.Cursor]
			r.Enabled = !r.Enabled
		}
	case "r", "R":
		m.ResetPending = true
	}
	return false, false
}

func (m *Menu) Render(width int) []string {
	title := "[Tools]   Skills"
	if m.Tab == 1 {
		title = "Tools   [Skills]"
	}
	lines := []string{title, "Space: toggle | Tab: switch | R: reset | Enter: save | Esc: cancel"}
	if m.ResetPending {
		lines = append(lines, "Reset global choices to defaults? Y: yes | N: no")
	}
	rows := m.Tabs[m.Tab]
	start := max(0, m.Cursor-8)
	for i := start; i < min(len(rows), start+16); i++ {
		r := rows[i]
		mark := " "
		if r.Enabled {
			mark = "x"
		}
		cursor := " "
		if i == m.Cursor {
			cursor = ">"
		}
		n, tokens := Characters(r.Context)
		visible := "hidden"
		if r.Visible && r.Enabled {
			visible = "visible"
		}
		loaded := ""
		if m.Tab == 1 && m.LoadedKnown {
			loaded = fmt.Sprintf("; loaded %d chars / ~%d tokens", r.LoadedChars, (r.LoadedChars+3)/4)
		}
		lines = append(lines, fmt.Sprintf("%s [%s] %s (%s; %s) — %d chars / ~%d tokens%s", cursor, mark, r.Name, r.Source, visible, n, tokens, loaded))
	}
	if len(rows) == 0 {
		lines = append(lines, "No entries.")
	}
	for tab, rs := range m.Tabs {
		chars := 0
		for _, r := range rs {
			if r.Enabled && r.Visible {
				n, _ := Characters(r.Context)
				chars += n
			}
		}
		label := []string{"Tools", "Skills"}[tab]
		lines = append(lines, fmt.Sprintf("%s enabled visible context: %d chars / ~%d tokens", label, chars, (chars+3)/4))
	}
	loaded := 0
	for _, r := range m.Tabs[1] {
		loaded += r.LoadedChars
	}
	if m.LoadedKnown {
		lines = append(lines, fmt.Sprintf("Last request loaded skill blocks: %d chars / ~%d tokens (separate from discovery)", loaded, (loaded+3)/4))
	} else {
		lines = append(lines, "Loaded skill counts: unknown until a model request in this session.")
	}
	lines = append(lines, "Estimates: 4 chars/token; discovery metadata and tool schemas, without shared wrappers.", "Loaded blocks can remain in old messages after a skill is disabled.", "Skill settings control discovery and invocation, not filesystem access.")
	for i, line := range lines {
		lines[i] = tui.Fit(strings.ReplaceAll(line, "\n", " "), width)
	}
	return lines
}
