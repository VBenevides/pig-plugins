package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

func Key(data string) string {
	switch data {
	case "\x1b":
		return "escape"
	case "\r", "\n":
		return "enter"
	case "\t":
		return "tab"
	case "\x7f", "\b":
		return "backspace"
	case "\x1b[Z":
		return "shift+tab"
	case " ":
		return "space"
	}
	if len(data) == 1 && data[0] > 0 && data[0] < 32 {
		switch data[0] {
		case 28:
			return "ctrl+\\"
		case 29:
			return "ctrl+]"
		case 30:
			return "ctrl+^"
		case 31:
			return "ctrl+_"
		}
		return "ctrl+" + string(rune('a'+data[0]-1))
	}
	if strings.HasPrefix(data, "\x1b[") {
		body := strings.TrimPrefix(data, "\x1b[")
		if len(body) > 0 {
			final := body[len(body)-1]
			parts := strings.Split(body[:len(body)-1], ";")
			mods := 1
			if len(parts) > 1 {
				mods, _ = strconv.Atoi(strings.Split(parts[1], ":")[0])
				if mods < 1 {
					mods = 1
				}
			}
			key := ""
			switch final {
			case 'A':
				key = "up"
			case 'B':
				key = "down"
			case 'C':
				key = "right"
			case 'D':
				key = "left"
			case 'H':
				key = "home"
			case 'F':
				key = "end"
			case '~':
				key = map[string]string{"1": "home", "2": "insert", "3": "delete", "4": "end", "5": "pageup", "6": "pagedown", "11": "f1", "12": "f2", "13": "f3", "14": "f4", "15": "f5", "17": "f6", "18": "f7", "19": "f8", "20": "f9", "21": "f10", "23": "f11", "24": "f12"}[parts[0]]
			case 'u':
				code, _ := strconv.Atoi(strings.Split(parts[0], ":")[0])
				key = map[int]string{27: "escape", 13: "enter", 9: "tab", 127: "backspace", 32: "space", 57348: "insert", 57349: "delete", 57350: "left", 57351: "right", 57352: "up", 57353: "down", 57354: "pageup", 57355: "pagedown", 57356: "home", 57357: "end"}[code]
				if key == "" && code >= 57364 && code <= 57375 {
					key = "f" + strconv.Itoa(code-57363)
				}
				if key == "" && code >= 32 && code <= utf8.MaxRune {
					key = strings.ToLower(string(rune(code)))
				}
			}
			if key != "" {
				return modifiers(key, mods-1)
			}
		}
	}
	if strings.HasPrefix(data, "\x1b") && utf8.RuneCountInString(data[1:]) == 1 {
		return "alt+" + strings.ToLower(data[1:])
	}
	if utf8.RuneCountInString(data) == 1 {
		return strings.ToLower(data)
	}
	return ""
}
func modifiers(key string, bits int) string {
	prefix := ""
	if bits&4 != 0 {
		prefix += "ctrl+"
	}
	if bits&1 != 0 {
		prefix += "shift+"
	}
	if bits&2 != 0 {
		prefix += "alt+"
	}
	if bits&8 != 0 {
		prefix += "super+"
	}
	return prefix + key
}
func Matches(data, spec string) bool {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "off" {
		return false
	}
	parts := strings.Split(spec, "+")
	base := parts[len(parts)-1]
	bits := 0
	seen := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		if seen[part] {
			return false
		}
		seen[part] = true
		switch part {
		case "ctrl":
			bits |= 4
		case "shift":
			bits |= 1
		case "alt":
			bits |= 2
		case "super":
			bits |= 8
		default:
			return false
		}
	}
	if base == "esc" {
		base = "escape"
	}
	if base == "return" {
		base = "enter"
	}
	if bits == 4 && len(base) == 1 && len(data) == 1 {
		r := base[0]
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if r >= 64 && r <= 127 && data[0] == r&31 {
			return true
		}
	}
	return Key(data) == modifiers(base, bits)
}

// Kitty distinguishes key presses, repeats, and releases.
func eventType(data string) string {
	if !strings.HasPrefix(data, "\x1b[") || !strings.HasSuffix(data, "u") {
		return ""
	}
	fields := strings.Split(data[2:len(data)-1], ";")
	if len(fields) < 2 {
		return ""
	}
	modifiers := strings.Split(fields[1], ":")
	if len(modifiers) < 2 {
		return ""
	}
	return modifiers[1]
}
func Released(data string) bool { return eventType(data) == "3" }
func Repeated(data string) bool { return eventType(data) == "2" }
