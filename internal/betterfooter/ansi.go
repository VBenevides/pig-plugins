// Package betterfooter implements the pure logic behind the better-footer extension (a port of
// pi-better-footer@0.1.3): ANSI-aware layout, session statistics, token speed, quota windows and their
// sources, the git and project readers, the settings file and the recent-model state.
package betterfooter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// RuneWidth is the number of terminal cells a rune occupies: 0 for combining marks and format characters,
// 2 for East Asian wide characters and emoji presentation, else 1. Private-use glyphs (Nerd Font icons) count 1.
func RuneWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x20 || r >= 0x7f && r < 0xa0:
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	case isWide(r):
		return 2
	}
	return 1
}

type runeRange struct{ lo, hi rune }

var wideRanges = []runeRange{
	{0x1100, 0x115f}, {0x231a, 0x231b}, {0x2329, 0x232a}, {0x23e9, 0x23ec}, {0x23f0, 0x23f0}, {0x23f3, 0x23f3},
	{0x25fd, 0x25fe}, {0x2614, 0x2615}, {0x2648, 0x2653}, {0x267f, 0x267f}, {0x2693, 0x2693}, {0x26a1, 0x26a1},
	{0x26aa, 0x26ab}, {0x26bd, 0x26be}, {0x26c4, 0x26c5}, {0x26ce, 0x26ce}, {0x26d4, 0x26d4}, {0x26ea, 0x26ea},
	{0x26f2, 0x26f3}, {0x26f5, 0x26f5}, {0x26fa, 0x26fa}, {0x26fd, 0x26fd}, {0x2705, 0x2705}, {0x270a, 0x270b},
	{0x2728, 0x2728}, {0x274c, 0x274c}, {0x274e, 0x274e}, {0x2753, 0x2755}, {0x2757, 0x2757}, {0x2795, 0x2797},
	{0x27b0, 0x27b0}, {0x27bf, 0x27bf}, {0x2b1b, 0x2b1c}, {0x2b50, 0x2b50}, {0x2b55, 0x2b55},
	{0x2e80, 0x303e}, {0x3041, 0x33ff}, {0x3400, 0x4dbf}, {0x4e00, 0xa4cf}, {0xa960, 0xa97f}, {0xac00, 0xd7a3},
	{0xf900, 0xfaff}, {0xfe10, 0xfe19}, {0xfe30, 0xfe6f}, {0xff00, 0xff60}, {0xffe0, 0xffe6},
	{0x1f004, 0x1f004}, {0x1f0cf, 0x1f0cf}, {0x1f18e, 0x1f18e}, {0x1f191, 0x1f19a}, {0x1f200, 0x1f320},
	{0x1f32d, 0x1f335}, {0x1f337, 0x1f37c}, {0x1f37e, 0x1f393}, {0x1f3a0, 0x1f3ca}, {0x1f3cf, 0x1f3d3},
	{0x1f3e0, 0x1f3f0}, {0x1f3f4, 0x1f3f4}, {0x1f3f8, 0x1f43e}, {0x1f440, 0x1f440}, {0x1f442, 0x1f4fc},
	{0x1f4ff, 0x1f53d}, {0x1f54b, 0x1f54e}, {0x1f550, 0x1f567}, {0x1f57a, 0x1f57a}, {0x1f595, 0x1f596},
	{0x1f5a4, 0x1f5a4}, {0x1f5fb, 0x1f64f}, {0x1f680, 0x1f6c5}, {0x1f6cc, 0x1f6cc}, {0x1f6d0, 0x1f6d2},
	{0x1f6d5, 0x1f6d7}, {0x1f6dc, 0x1f6df}, {0x1f6eb, 0x1f6ec}, {0x1f6f4, 0x1f6fc}, {0x1f7e0, 0x1f7eb},
	{0x1f7f0, 0x1f7f0}, {0x1f90c, 0x1f93a}, {0x1f93c, 0x1f945}, {0x1f947, 0x1f9ff}, {0x1fa70, 0x1faff},
	{0x20000, 0x2fffd}, {0x30000, 0x3fffd},
}

func isWide(r rune) bool {
	if r < 0x1100 {
		return false
	}
	lo, hi := 0, len(wideRanges)
	for lo < hi {
		mid := (lo + hi) / 2
		switch {
		case r < wideRanges[mid].lo:
			hi = mid
		case r > wideRanges[mid].hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// escapeLen returns the byte length of the terminal escape sequence that starts at s[0] (which is ESC):
// CSI (ESC [ ... final), OSC (ESC ] ... BEL or ST), or a two-byte escape. An unterminated sequence runs to the end.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	}
	return 2
}

// SanitizePlain removes terminal commands and C0/C1 controls from untrusted text.
// CR, LF and tab become spaces. Unlike Sanitize, it also removes SGR styling;
// use it for metadata and errors, not already-themed footer statuses.
func SanitizePlain(text string) string {
	const (
		normal = iota
		escape
		csi
		controlString
	)
	state, osc := normal, false
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		// Also recognize C1 controls in legacy, non-UTF-8 terminal text.
		if r == utf8.RuneError && size == 1 && text[i] >= 0x80 && text[i] <= 0x9f {
			r = rune(text[i])
		}
		i += size
		if state == controlString {
			if r == 0x9c || osc && r == '\a' {
				state = normal
			} else if r == '\x1b' && i < len(text) && text[i] == '\\' {
				i++
				state = normal
			}
			continue
		}
		if r == '\x1b' {
			state = escape
			continue
		}
		switch r {
		case 0x9b:
			state = csi
			continue
		case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
			state, osc = controlString, r == 0x9d
			continue
		}
		switch state {
		case escape:
			switch r {
			case '[':
				state = csi
			case ']', 'P', 'X', '^', '_':
				state, osc = controlString, r == ']'
			default:
				if r >= 0x30 && r <= 0x7e {
					state = normal
				}
			}
			continue
		case csi:
			if r >= 0x40 && r <= 0x7e {
				state = normal
			}
			continue
		}
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			out.WriteByte(' ')
		case r < 0x20 || r >= 0x7f && r <= 0x9f:
			continue
		default:
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(spaces.ReplaceAllString(out.String(), " "))
}

// VisibleWidth is the number of terminal cells of s, ignoring ANSI escape sequences.
func VisibleWidth(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i += escapeLen(s[i:])
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		width += RuneWidth(r)
		i += size
	}
	return width
}

// Truncate cuts s to at most max cells without splitting a character, keeping every escape sequence seen
// before the cut. A cut text ends with an SGR reset, and closes an open OSC-8 hyperlink, so nothing leaks into
// the next row. Control characters other than escapes are dropped.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if VisibleWidth(s) <= max {
		return s
	}
	var out strings.Builder
	width := 0
	styled, link := false, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			n := escapeLen(s[i:])
			seq := s[i : i+n]
			out.WriteString(seq)
			switch {
			case strings.HasPrefix(seq, "\x1b]8;"):
				// "ESC ] 8 ; params ; URI ST": an empty URI closes the link.
				body := strings.TrimSuffix(strings.TrimSuffix(seq[2:], "\x07"), "\x1b\\")
				parts := strings.SplitN(body, ";", 3)
				link = len(parts) == 3 && parts[2] != ""
			case strings.HasPrefix(seq, "\x1b["):
				styled = true
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := RuneWidth(r)
		if width+w > max {
			break
		}
		if !unicode.IsControl(r) {
			out.WriteString(s[i : i+size])
		}
		width += w
		i += size
	}
	if link {
		out.WriteString("\x1b]8;;\x1b\\")
	}
	if styled {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}
