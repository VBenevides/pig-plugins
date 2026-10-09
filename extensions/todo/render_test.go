package todoext

import (
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
	"testing"
)

func TestViewerBoundsAndControlFiltering(t *testing.T) {
	v := &viewer{phases: []tasks.Phase{{Name: "界", Tasks: []tasks.Task{{Content: "界😀\x1b]52;c;clipboard\a\u009b2J", Status: "pending"}, {Content: strings.Repeat("界", 100), Status: "completed"}}}}}
	for _, width := range []int{1, 8, 80} {
		for _, line := range v.Render(width) {
			if tui.Width(line) > width || strings.ContainsAny(line, "\x1b\a\u009b") {
				t.Fatalf("unsafe or overwide viewer row at %d: %q", width, line)
			}
		}
	}
}
