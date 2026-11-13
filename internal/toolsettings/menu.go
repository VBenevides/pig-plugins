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
	// Exposure describes registration or lifecycle, independently of permission and current visibility.
	Exposure string
	// Variant is empty for ordinary tools, or hashline/native for file-tool alternatives.
	Variant string
	// Group is the category shown as a collapsible header. Empty means the row has no category.
	Group string
	// Initial is Enabled as shown when the dialog opened; only changed rows are saved.
	Initial bool
}

// Menu keeps edits local until the user saves. Reset requires an explicit second key.
type Menu struct {
	Tabs                          [2][]Row
	Tab, Cursor                   int
	ResetPending                  bool
	Reset                         bool
	LoadedKnown                   bool
	OtherLoadedChars              int
	ShowHelp                      bool
	PresetPending                 bool
	RestrictTools, RestrictSkills bool
	NativeFiles                   bool
	ProfileOverride               string
	// Collapsed holds the folded categories of the Tools tab.
	Collapsed map[string]bool
	// KeepSkills lists enabled skills omitted from the dialog; a restricted skill allowlist still includes them.
	KeepSkills []string
	// ApplyAll records an explicit preset action, including unchanged checkboxes.
	ApplyAll bool
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
		case "4":
			m.ApplyPreset(BenchmarkPreset)
			m.PresetPending = false
		case "escape", "p", "P":
			m.PresetPending = false
		}
		return false, false
	}
	if m.ProfileOverride != "" && (key == " " || key == "space" || key == "p" || key == "P" || key == "r" || key == "R") {
		return false, false
	}
	switch key {
	case "escape":
		return true, false
	case "enter":
		return true, true
	case "tab", "shift+tab":
		m.Tab, m.Cursor = 1-m.Tab, 0
	case "left", "right":
		if m.Tab == 0 && m.hasGroups() {
			m.fold(key == "left")
		} else {
			m.Tab, m.Cursor = 1-m.Tab, 0
		}
	case "up":
		m.Cursor = max(0, m.Cursor-1)
	case "down":
		m.Cursor = min(max(0, len(m.entries())-1), m.Cursor+1)
	case " ", "space":
		es := m.entries()
		if m.Cursor >= len(es) {
			break
		}
		if es[m.Cursor].header {
			m.toggleGroup(es[m.Cursor].group)
			break
		}
		r := &m.Tabs[m.Tab][es[m.Cursor].row]
		if m.Tab == 0 && r.Variant != "" && !m.SelectedVariant(*r) {
			m.SwitchFileVariant(r.Variant == "native")
			r.Enabled = true
		} else {
			r.Enabled = !r.Enabled
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
	label := m.presetLabel()
	if m.ProfileOverride != "" {
		label = m.ProfileOverride + " (tools locked)"
	}
	lines := []string{rule("╭", "╮"), line("TOOL SETTINGS  ·  " + label + "  ·  global"), line("Enter saves + reloads"), line(tabs), line("")}
	const pageSize = 8
	es := m.entries()
	cursor := min(max(0, m.Cursor), max(0, len(es)-1))
	start := min(max(0, cursor-pageSize/2), max(0, len(es)-pageSize))
	end := min(len(es), start+pageSize)
	nameWidth := max(8, inner-33)
	wide := inner >= 48
	if wide {
		lines = append(lines, line("      "+tui.Pad("NAME", nameWidth)+fmt.Sprintf(" %-7s %8s %8s", "STATE", "CHARS", "TOKENS")))
	}
	for i := start; i < end; i++ {
		pointer := " "
		if i == cursor {
			pointer = "›"
		}
		lines = append(lines, line(m.renderEntry(es[i], pointer, nameWidth, wide)))
	}
	if len(es) == 0 {
		lines = append(lines, line("No entries in this tab."))
	} else {
		lines = append(lines, line(fmt.Sprintf("%d–%d of %d  ·  ↑↓ navigate", start+1, end, len(es))))
	}
	lines = append(lines, rule("├", "┤"))
	if len(es) > 0 && es[cursor].header {
		group := es[cursor].group
		enabled, total := m.groupState(group)
		chars := m.groupChars(group)
		lines = append(lines, line(fmt.Sprintf("%s  ·  category  ·  %d of %d enabled", group, enabled, total)))
		lines = append(lines, line(fmt.Sprintf("Selected: %s chars / ~%s tokens", number(chars), number((chars+3)/4))))
		lines = append(lines, line("Space toggles the whole category  ·  ←/→ fold or unfold"))
	} else if len(es) > 0 {
		r := m.Tabs[m.Tab][es[cursor].row]
		source := r.Source
		if source == "" {
			source = "unknown source"
		}
		exposure := ""
		if r.Exposure != "" {
			exposure = "  ·  " + r.Exposure
		}
		lines = append(lines, line(r.Name+exposure+"  ·  "+source))
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
	// Skill bodies from user-only skills can remain in history even though their metadata is omitted.
	if m.ShowHelp {
		loaded := m.OtherLoadedChars
		for _, r := range m.Tabs[1] {
			loaded += r.LoadedChars
		}
		if m.LoadedKnown {
			lines = append(lines, line(fmt.Sprintf("Last request loaded skill blocks: %s chars / ~%s tokens", number(loaded), number((loaded+3)/4))))
		} else {
			lines = append(lines, line("Loaded skill counts: unknown until the first model request."))
		}
		deferred, chars := 0, 0
		for _, r := range m.Tabs[0] {
			if r.Enabled && !r.Visible && (r.Exposure == "deferred" || r.Exposure == "codemode") {
				deferred++
				n, _ := Characters(r.Context)
				chars += n
			}
		}
		lines = append(lines, line(fmt.Sprintf("Discoverable: %d tools / %s schema chars (not declared)", deferred, number(chars))))
		for _, text := range []string{"Checkboxes are permissions, not schema activation.", "Totals estimate active schemas and visible skill metadata.", "Tokens are estimates: 4 characters per token.", "Provider wrappers and shared instructions are excluded.", "Loaded skill blocks are counted separately from metadata.", "Old messages and user-invoked skills can still use context.", "Skill choices do not restrict filesystem access."} {
			lines = append(lines, line(text))
		}
	}
	lines = append(lines, rule("├", "┤"))
	if m.ResetPending {
		lines = append(lines, line("Reset global choices to defaults?"), line("Y confirm  ·  N cancel"))
	} else if m.PresetPending {
		lines = append(lines, line("1 default  · default tools + all skills; Hashline"), line("2 minimum  · read/bash/edit/write; no skills; Hashline"), line("3 native   · read/bash/edit/write; no skills; native files"), line("4 default-benchmark · default without ask_user_question"), line("Choose 1, 2, 3, or 4  ·  Esc back  ·  Enter saves afterward"))
	} else {
		lines = append(lines, line("Space toggle  ·  ←/→ fold  ·  Tab switch  ·  P profiles"), line("Enter save  ·  Esc cancel  ·  ? help  ·  R reset"))
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

// renderEntry formats one list line. Group headers show a fold arrow, a tri-state checkbox, and the totals
// of their enabled rows; rows inside a group are indented under the header.
func (m *Menu) renderEntry(e entry, pointer string, nameWidth int, wide bool) string {
	if e.header {
		enabled, total := m.groupState(e.group)
		mark, arrow := " ", "v"
		switch {
		case total > 0 && enabled == total:
			mark = "x"
		case enabled > 0:
			mark = "~"
		}
		if m.Collapsed[e.group] {
			arrow = ">"
		}
		chars := m.groupChars(e.group)
		name := fmt.Sprintf("%s %s (%d/%d)", arrow, e.group, enabled, total)
		if !wide {
			return fmt.Sprintf("%s [%s] %s", pointer, mark, name)
		}
		return fmt.Sprintf("%s [%s] %s %-7s %8s %8s", pointer, mark, tui.Pad(tui.Fit(name, nameWidth), nameWidth), "", number(chars), "~"+number((chars+3)/4))
	}
	r := m.Tabs[m.Tab][e.row]
	mark := " "
	if r.Enabled {
		mark = "x"
	}
	state := "off"
	if r.Enabled {
		state = "hidden"
		if r.Visible {
			state = "visible"
		}
	}
	if r.Enabled && r.Exposure != "" {
		switch r.Exposure {
		case "deferred", "codemode":
			state = "defer"
			if r.Visible {
				state = "loaded"
			}
		case "conditional":
			state = "cond"
			if r.Visible {
				state = "active"
			}
		case "backend":
			state = "backend"
		case "direct":
			if r.Visible {
				state = "direct"
			}
		}
	}
	if r.Enabled && r.Variant != "" && r.Context == "" {
		state = "pending"
	}
	name := r.Name
	if e.group != "" {
		name = "    " + name
	}
	n, tokens := Characters(r.Context)
	if !wide {
		return fmt.Sprintf("%s [%s] %s", pointer, mark, name)
	}
	chars, estimate := number(n), "~"+number(tokens)
	if r.Variant != "" && r.Context == "" {
		chars, estimate = "—", "—"
	}
	return fmt.Sprintf("%s [%s] %s %-7s %8s %8s", pointer, mark, tui.Pad(tui.Fit(name, nameWidth), nameWidth), state, chars, estimate)
}
