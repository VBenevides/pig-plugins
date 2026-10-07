// Package tui contains conservative terminal text and key handling for remote components.
package tui

import (
	"strings"
	"unicode"
)

func cells(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d || r == 0xfe0f {
		return 0
	}
	if r >= 0x2e80 || r >= 0x2300 && r <= 0x27ff {
		return 2
	}
	return 1
}
func control(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) && r != 0x200d }

// Fit removes terminal controls and never exceeds the requested columns. Ambiguous wide glyphs are conservatively counted.
func Fit(text string, width int) string {
	var out strings.Builder
	used := 0
	for _, r := range text {
		repeat := 1
		if r == '\t' {
			r = ' '
			repeat = 4
		}
		if control(r) {
			continue
		}
		for range repeat {
			n := cells(r)
			if used+n > max(0, width) {
				return out.String()
			}
			out.WriteRune(r)
			used += n
		}
	}
	return out.String()
}
func Width(text string) int {
	n := 0
	for _, r := range text {
		if r == '\t' {
			n += 4
		} else if !control(r) {
			n += cells(r)
		}
	}
	return n
}
func Wrap(text string, width, limit int) []string {
	if width < 1 || limit < 1 {
		return nil
	}
	var lines []string
	var line strings.Builder
	used := 0
	for _, r := range text {
		if r == '\n' {
			lines = append(lines, line.String())
			line.Reset()
			used = 0
			if len(lines) >= limit {
				return lines
			}
			continue
		}
		repeat := 1
		if r == '\t' {
			r = ' '
			repeat = 4
		}
		if control(r) {
			continue
		}
		for range repeat {
			n := cells(r)
			if used+n > width {
				lines = append(lines, line.String())
				line.Reset()
				used = 0
				if len(lines) >= limit {
					return lines
				}
			}
			if n <= width {
				line.WriteRune(r)
				used += n
			}
		}
	}
	if len(lines) < limit {
		lines = append(lines, line.String())
	}
	return lines
}
func Pad(text string, width int) string {
	text = Fit(text, width)
	return text + strings.Repeat(" ", max(0, width-Width(text)))
}
