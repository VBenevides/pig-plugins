package ask

import (
	"fmt"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
)

func (s *State) Render(width int) []string {
	width = max(1, width)
	if s.collapsed {
		return []string{tui.Fit("Questions collapsed — "+s.CollapseKey+" to expand; Esc cancels", width)}
	}
	inner := max(1, width-4)
	var lines []string
	if s.tabs() > 1 {
		var tabs []string
		for i, q := range s.Params.Questions {
			label := firstHeader(q.Header, i)
			if _, ok := s.answers[i]; ok {
				label += " +"
			}
			if i == s.tab {
				label = "[" + label + "]"
			}
			tabs = append(tabs, label)
		}
		submit := "Submit"
		if s.review() {
			submit = "[Submit]"
		}
		tabs = append(tabs, submit)
		lines = append(lines, strings.Join(tabs, " | "), "")
	}
	if s.review() {
		lines = append(lines, s.reviewLines(inner)...)
	} else {
		lines = append(lines, s.questionLines(width, inner)...)
	}
	if s.noteMode {
		label := "Notes"
		if s.review() {
			label = "Global note"
		}
		lines = append(lines, "", label+" (Enter/Esc closes; Shift+Enter adds a line)")
		lines = append(lines, tui.Wrap(s.noteEditor.Display(), inner, 6)...)
	} else if note := s.notes[s.tab]; note != "" {
		lines = append(lines, tui.Wrap("Note: "+note, inner, 2)...)
	}
	lines = append(lines, "", "Up/Down select; Enter confirm; n notes; Esc cancel")
	if s.tabs() > 1 {
		lines = append(lines, "Tab/Shift+Tab switch questions and Submit")
	}
	if s.input() {
		lines = append(lines, "Shift+Enter newline; Ctrl+U clear; Ctrl+G editor")
	}
	border := "+" + strings.Repeat("-", max(0, width-2)) + "+"
	out := make([]string, 0, len(lines)+2)
	out = append(out, tui.Fit(border, width))
	for _, line := range lines {
		out = append(out, tui.Fit("| "+tui.Pad(line, inner)+" |", width))
	}
	return append(out, tui.Fit(border, width))
}
func (s *State) reviewLines(width int) []string {
	lines := []string{"Review answers"}
	var missing []string
	for i, q := range s.Params.Questions {
		if a, ok := s.answers[i]; ok {
			lines = append(lines, tui.Wrap(firstHeader(q.Header, i)+": "+Scalar(a), width, 2)...)
		} else {
			missing = append(missing, firstHeader(q.Header, i))
		}
	}
	if len(missing) > 0 {
		lines = append(lines, tui.Wrap("Unanswered: "+strings.Join(missing, ", ")+". Partial submit is allowed.", width, 2)...)
	}
	for i, label := range []string{"Submit answers", "Cancel"} {
		prefix := "  "
		if s.row == i {
			prefix = "> "
		}
		lines = append(lines, prefix+label)
	}
	return lines
}
func (s *State) questionLines(width, inner int) []string {
	q := s.question()
	lines := tui.Wrap(q.Question, inner, 4)
	lines = append(lines, "")
	hasPreview := false
	for _, o := range q.Options {
		hasPreview = hasPreview || o.Preview != ""
	}
	showPreview := !q.Multi && hasPreview && !s.input()
	sideBySide := showPreview && width >= 100
	optionWidth := inner
	if sideBySide {
		optionWidth = inner / 2
	}
	var options []string
	for i, o := range q.Options {
		prefix := "  "
		if s.row == i {
			prefix = "> "
		}
		check := ""
		if q.Multi {
			check = "[ ] "
			if checked := s.checked[s.tab]; len(checked) > i && checked[i] {
				check = "[x] "
			}
		}
		options = append(options, tui.Wrap(fmt.Sprintf("%s%s%d. %s", prefix, check, i+1, o.Label), optionWidth, 2)...)
		if o.Description != "" {
			options = append(options, tui.Wrap("    "+o.Description, optionWidth, 2)...)
		}
	}
	prefix := "  "
	if s.input() {
		prefix = "> "
	}
	options = append(options, prefix+"Type something.")
	if s.input() {
		options = append(options, tui.Wrap("  "+s.draft().Display(), inner, 4)...)
	}
	if q.Multi {
		prefix = "  "
		if s.row == len(q.Options)+1 {
			prefix = "> "
		}
		label := "Next"
		if s.tab == len(s.Params.Questions)-1 {
			label = "Submit"
		}
		options = append(options, prefix+label)
	}
	if !showPreview {
		return append(lines, options...)
	}
	preview := "No preview available"
	if s.row < len(q.Options) && q.Options[s.row].Preview != "" {
		preview = q.Options[s.row].Preview
	}
	if sideBySide {
		left := optionWidth
		right := max(1, inner-left-3)
		pane := previewLines(preview, right, 20)
		for i := range max(len(options), len(pane)) {
			a, b := "", ""
			if i < len(options) {
				a = options[i]
			}
			if i < len(pane) {
				b = pane[i]
			}
			lines = append(lines, tui.Pad(a, left)+" | "+b)
		}
		return lines
	}
	lines = append(lines, options...)
	lines = append(lines, "--- Preview ---")
	return append(lines, previewLines(preview, inner, 15)...)
}
func previewLines(text string, width, limit int) []string {
	var lines []string
	for line := range strings.Lines(text) {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		lines = append(lines, tui.Wrap(strings.TrimSuffix(line, "\n"), width, limit-len(lines))...)
		if len(lines) >= limit {
			break
		}
	}
	return lines
}
func firstHeader(header string, index int) string {
	if header != "" {
		return header
	}
	return fmt.Sprintf("Q%d", index+1)
}
