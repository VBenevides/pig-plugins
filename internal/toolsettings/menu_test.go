package toolsettings

import (
	"fmt"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/tui"
)

func TestMenuLayoutAndScrolling(t *testing.T) {
	m := &Menu{}
	for i := range 30 {
		m.Tabs[0] = append(m.Tabs[0], Row{Name: fmt.Sprintf("tool_%02d", i), Source: "example-extension", Context: strings.Repeat("x", 1027), Enabled: i%2 == 0, Visible: true})
	}
	for _, width := range []int{0, 1, 20, 40, 80, 160} {
		lines := m.Render(width)
		for _, line := range lines {
			// Normalize the known one-cell frame glyphs before using the conservative text-width helper.
			plain := strings.NewReplacer("│", "|", "─", "-", "╭", "+", "╮", "+", "├", "+", "┤", "+", "╰", "+", "╯", "+").Replace(line)
			if tui.Width(plain) > width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
		if width == 80 {
			text := strings.Join(lines, "\n")
			for _, part := range []string{"[ Tools 15/30 ]", "STATE", "CHARS", "TOKENS", "1,027", "1–8 of 30", "tool_00  ·  example-extension"} {
				if !strings.Contains(text, part) {
					t.Fatalf("missing %q:\n%s", part, text)
				}
			}
			if strings.Contains(text, "filesystem") {
				t.Fatal("long help appears by default")
			}
			if len(lines) > 24 {
				t.Fatalf("menu too tall: %d", len(lines))
			}
		}
	}
	for range 29 {
		m.Handle("down")
	}
	text := strings.Join(m.Render(80), "\n")
	if !strings.Contains(text, "23–30 of 30") || !strings.Contains(text, "› [ ] tool_29") {
		t.Fatal(text)
	}
	m.Handle("?")
	if !strings.Contains(strings.Join(m.Render(80), "\n"), "filesystem access") {
		t.Fatal("help not shown")
	}
	m.Handle("?")
	if m.ShowHelp {
		t.Fatal("help did not close")
	}
	m.Handle("r")
	if !strings.Contains(strings.Join(m.Render(80), "\n"), "Y confirm") {
		t.Fatal("reset prompt missing")
	}
}

func TestNumberGrouping(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want string
	}{{0, "0"}, {999, "999"}, {1027, "1,027"}, {1234567, "1,234,567"}} {
		if got := number(tt.n); got != tt.want {
			t.Errorf("number(%d)=%s", tt.n, got)
		}
	}
}
