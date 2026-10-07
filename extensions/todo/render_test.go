package todoext

import (
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
	"testing"
)

func TestViewerBoundsAndControlFiltering(t *testing.T) {
	v := &viewer{items: []tasks.Item{{ID: 1, Text: "界😀\x1b]52;c;clipboard\a\u009b2J"}, {ID: 2, Text: strings.Repeat("界", 100), Done: true}}}
	for _, width := range []int{1, 8, 80} {
		for _, line := range v.Render(width) {
			if tui.Width(line) > width || strings.ContainsAny(line, "\x1b\a\u009b") {
				t.Fatalf("unsafe or overwide viewer row at %d: %q", width, line)
			}
		}
	}
}
