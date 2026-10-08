package betterfooter

import (
	"strings"
	"testing"
)

func TestSanitizePlainRemovesTerminalCommands(t *testing.T) {
	for _, command := range []string{
		"\x1b]52;c;secret\x1b\\",
		"\x1b]52;c;secret\a",
		"\x1b[2J", "\x1b[31m", "\x1b(B",
		"\x1bPsecret\x1b\\", "\x1bXsecret\x1b\\",
		"\x1b^secret\x1b\\", "\x1b_secret\x1b\\",
		"\u009d52;c;secret\u009c", "\u009b2J",
		"\x9d52;c;secret\x9c", "\x9b2J",
	} {
		t.Run(command, func(t *testing.T) {
			if got := SanitizePlain("before" + command + "after"); got != "beforeafter" {
				t.Fatalf("sanitized command = %q", got)
			}
		})
	}
	for _, command := range []string{"\x1b", "\x1b[", "\x1b]52;c;secret", "\x1bPsecret"} {
		if got := SanitizePlain("before" + command); got != "before" {
			t.Fatalf("unterminated command = %q", got)
		}
	}
	var controls strings.Builder
	for r := rune(0); r <= 0x9f; r++ {
		if r < 0x20 || r >= 0x7f {
			controls.WriteRune(r)
		}
	}
	if got := SanitizePlain(controls.String()); got != "" {
		t.Fatalf("control characters survived: %q", got)
	}
	if got := SanitizePlain(" 日本語\r\n\t  café "); got != "日本語 café" {
		t.Fatalf("text changed unexpectedly: %q", got)
	}
}

func TestStatusSanitizerRetainsThemeANSI(t *testing.T) {
	status := "\x1b[32mready\x1b[39m"
	if got := Sanitize(status); got != status {
		t.Fatalf("status styling lost: %q", got)
	}
	if got := SanitizePlain(status); got != "ready" {
		t.Fatalf("plain text retained styling: %q", got)
	}
}
