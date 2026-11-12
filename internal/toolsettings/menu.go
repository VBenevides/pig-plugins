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
	DefaultEnabled            bool
	// Variant is empty for ordinary tools, or hashline/native for file-tool alternatives.
	Variant string
}

// Menu keeps edits local until the user saves. Reset requires an explicit second key.
type Menu struct {
	Tabs                          [2][]Row
	Tab, Cursor                   int
	ResetPending                  bool
	Reset                         bool
	LoadedKnown                   bool
	ShowHelp                      bool
	PresetPending                 bool
	RestrictTools, RestrictSkills bool
	NativeFiles                   bool
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
	if m.PresetPending {
		switch key {
		case "1":
			m.ApplyPreset(DefaultPreset)
			m.PresetPending = false
		case "2":
			m.ApplyPreset(MinimumPreset)
			m.PresetPending = false
		case "3":
			m.ApplyPreset(NativePreset)
			m.PresetPending = false
		case "escape", "p", "P":
			m.PresetPending = false
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
			if m.Tab == 0 && r.Variant != "" && !m.SelectedVariant(*r) {
				m.SwitchFileVariant(r.Variant == "native")
				r.Enabled = true
			} else {
				r.Enabled = !r.Enabled
			}
		}
	case "?":
		m.ShowHelp = !m.ShowHelp
	case "p", "P":
		m.PresetPending = true
	case "r", "R":
		m.ResetPending = true
	}
	return false, false
}

func (m *Menu) Render(width int) []string {
	if width <= 0 {
		return nil
	}
	width = min(width, 96)
	inner := max(0, width-4)
	line := func(text string) string {
		if width < 4 {
			return tui.Fit(text, width)
		}
		return "│ " + tui.Pad(tui.Fit(text, inner), inner) + " │"
	}
	rule := func(left, right string) string {
		// Box-drawing characters occupy one terminal cell. Fit's conservative width rules treat them as wide.
		if width == 1 {
			return left
		}
		return left + strings.Repeat("─", width-2) + right
	}
	rows := m.Tabs[m.Tab]
	counts := [2]int{}
	for tab, rs := range m.Tabs {
		for _, r := range rs {
			if r.Enabled {
				counts[tab]++
			}
		}
	}
	tabs := fmt.Sprintf("[ Tools %d/%d ]   Skills %d/%d", counts[0], len(m.Tabs[0]), counts[1], len(m.Tabs[1]))
	if m.Tab == 1 {
		tabs = fmt.Sprintf("Tools %d/%d   [ Skills %d/%d ]", counts[0], len(m.Tabs[0]), counts[1], len(m.Tabs[1]))
	}
	lines := []string{rule("╭", "╮"), line("TOOL SETTINGS  ·  " + m.presetLabel() + "  ·  global"), line(tabs), line("")}
	const pageSize = 8
	cursor := min(max(0, m.Cursor), max(0, len(rows)-1))
	start := min(max(0, cursor-pageSize/2), max(0, len(rows)-pageSize))
	end := min(len(rows), start+pageSize)
	nameWidth := max(8, inner-33)
	wide := inner >= 48
	if wide {
		lines = append(lines, line("      "+tui.Pad("NAME", nameWidth)+fmt.Sprintf(" %-7s %8s %8s", "STATE", "CHARS", "TOKENS")))
	}
	for i := start; i < end; i++ {
		r := rows[i]
		mark, pointer := " ", " "
		if r.Enabled {
			mark = "x"
		}
		if i == cursor {
			pointer = "›"
		}
		state := "off"
		if r.Enabled {
			state = "hidden"
			if r.Visible {
				state = "visible"
			}
		}
		if r.Enabled && r.Variant != "" && r.Context == "" {
			state = "pending"
		}
		n, tokens := Characters(r.Context)
		text := fmt.Sprintf("%s [%s] %s", pointer, mark, r.Name)
		if wide {
			chars, estimate := number(n), "~"+number(tokens)
			if r.Variant != "" && r.Context == "" {
				chars, estimate = "—", "—"
			}
			text = fmt.Sprintf("%s [%s] %s %-7s %8s %8s", pointer, mark, tui.Pad(tui.Fit(r.Name, nameWidth), nameWidth), state, chars, estimate)
		}
		lines = append(lines, line(text))
	}
	if len(rows) == 0 {
		lines = append(lines, line("No entries in this tab."))
	} else {
		lines = append(lines, line(fmt.Sprintf("%d–%d of %d  ·  ↑↓ navigate", start+1, end, len(rows))))
	}
	lines = append(lines, rule("├", "┤"))
	if len(rows) > 0 {
		r := rows[cursor]
		source := r.Source
		if source == "" {
			source = "unknown source"
		}
		lines = append(lines, line(r.Name+"  ·  "+source))
		n, tokens := Characters(r.Context)
		if r.Variant != "" && r.Context == "" {
			lines = append(lines, line("Schema counts available after save and reload."))
		} else {
			lines = append(lines, line(fmt.Sprintf("Selected: %s chars / ~%s tokens", number(n), number(tokens))))
		}
		if r.Variant != "" {
			lines = append(lines, line("Space selects a file-tool implementation for read/edit/write together."))
		}
		if m.Tab == 1 && m.LoadedKnown {
			lines = append(lines, line(fmt.Sprintf("Loaded in last request: %s chars / ~%s tokens", number(r.LoadedChars), number((r.LoadedChars+3)/4))))
		}
	}
	for tab, rs := range m.Tabs {
		chars := 0
		for _, r := range rs {
			if r.Enabled && r.Visible {
				n, _ := Characters(r.Context)
				chars += n
			}
		}
		lines = append(lines, line(fmt.Sprintf("%s: %s chars / ~%s tokens", []string{"Tools", "Skills"}[tab], number(chars), number((chars+3)/4))))
	}
	if m.ShowHelp {
		loaded := 0
		for _, r := range m.Tabs[1] {
			loaded += r.LoadedChars
		}
		if m.LoadedKnown {
			lines = append(lines, line(fmt.Sprintf("Last request loaded skill blocks: %s chars / ~%s tokens", number(loaded), number((loaded+3)/4))))
		} else {
			lines = append(lines, line("Loaded skill counts: unknown until the first model request."))
		}
		for _, text := range []string{"Totals count enabled, visible discovery context.", "Tokens are estimates: 4 characters per token.", "Tool schemas and skill metadata exclude shared wrappers.", "Loaded skill blocks are counted separately from metadata.", "Disabled skills can remain in old messages.", "Skill choices do not restrict filesystem access."} {
			lines = append(lines, line(text))
		}
	}
	lines = append(lines, rule("├", "┤"))
	if m.ResetPending {
		lines = append(lines, line("Reset global choices to defaults?"), line("Y confirm  ·  N cancel"))
	} else if m.PresetPending {
		lines = append(lines, line("1 Default  · default tools + all skills; Hashline"), line("2 Minimum  · read/bash/edit/write; no skills; Hashline"), line("3 Native   · read/bash/edit/write; no skills; native files"), line("Choose 1, 2, or 3  ·  Esc back  ·  Enter saves afterward"))
	} else {
		lines = append(lines, line("Space toggle  ·  Tab switch  ·  P profiles"), line("Enter save  ·  Esc cancel  ·  ? help  ·  R reset"))
	}
	return append(lines, rule("╰", "╯"))
}

func number(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
