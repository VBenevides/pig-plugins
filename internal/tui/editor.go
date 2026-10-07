package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Editor stores byte-indexed cursor state. It does not execute terminal escapes from pasted text.
type Editor struct {
	Text   string
	Cursor int
}

func (e *Editor) Set(text string) { e.Text = text; e.Cursor = len(text) }
func (e *Editor) lineBounds() (int, int) {
	start := strings.LastIndexByte(e.Text[:e.Cursor], '\n') + 1
	end := strings.IndexByte(e.Text[e.Cursor:], '\n')
	if end < 0 {
		end = len(e.Text)
	} else {
		end += e.Cursor
	}
	return start, end
}
func (e *Editor) Vertical(direction int) bool {
	start, end := e.lineBounds()
	column := utf8.RuneCountInString(e.Text[start:e.Cursor])
	var target string
	offset := 0
	if direction < 0 {
		if start == 0 {
			return false
		}
		targetEnd := start - 1
		offset = strings.LastIndexByte(e.Text[:targetEnd], '\n') + 1
		target = e.Text[offset:targetEnd]
	} else {
		if end == len(e.Text) {
			return false
		}
		offset = end + 1
		target = e.Text[offset:]
		if i := strings.IndexByte(target, '\n'); i >= 0 {
			target = target[:i]
		}
	}
	e.Cursor = offset
	for _, r := range target {
		if column == 0 {
			break
		}
		e.Cursor += utf8.RuneLen(r)
		column--
	}
	return true
}
func (e *Editor) insert(text string) {
	e.Text = e.Text[:e.Cursor] + text + e.Text[e.Cursor:]
	e.Cursor += len(text)
}
func (e *Editor) Handle(data string) {
	key := Key(data)
	switch key {
	case "left":
		if e.Cursor > 0 {
			_, n := utf8.DecodeLastRuneInString(e.Text[:e.Cursor])
			e.Cursor -= n
		}
	case "right":
		if e.Cursor < len(e.Text) {
			_, n := utf8.DecodeRuneInString(e.Text[e.Cursor:])
			e.Cursor += n
		}
	case "home", "ctrl+a":
		e.Cursor, _ = e.lineBounds()
	case "end", "ctrl+e":
		_, e.Cursor = e.lineBounds()
	case "up":
		e.Vertical(-1)
	case "down":
		e.Vertical(1)
	case "backspace":
		if e.Cursor > 0 {
			_, n := utf8.DecodeLastRuneInString(e.Text[:e.Cursor])
			e.Text = e.Text[:e.Cursor-n] + e.Text[e.Cursor:]
			e.Cursor -= n
		}
	case "delete":
		if e.Cursor < len(e.Text) {
			_, n := utf8.DecodeRuneInString(e.Text[e.Cursor:])
			e.Text = e.Text[:e.Cursor] + e.Text[e.Cursor+n:]
		}
	case "ctrl+u":
		e.Set("")
	case "shift+enter":
		e.insert("\n")
	default:
		if strings.HasPrefix(data, "\x1b[200~") {
			text := strings.TrimSuffix(strings.TrimPrefix(data, "\x1b[200~"), "\x1b[201~")
			text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "")
			e.insert(cleanInput(text))
			return
		}
		if strings.HasPrefix(data, "\x1b") || strings.HasPrefix(key, "ctrl+") || strings.HasPrefix(key, "alt+") {
			return
		}
		e.insert(cleanInput(data))
	}
}
func cleanInput(text string) string {
	var out strings.Builder
	for _, r := range text {
		if r < ' ' && r != '\n' && r != '\t' || r == 127 || unicode.Is(unicode.Cf, r) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
func (e *Editor) Display() string { return e.Text[:e.Cursor] + "|" + e.Text[e.Cursor:] }
