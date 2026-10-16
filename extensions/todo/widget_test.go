package todoext

import (
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"reflect"
	"strings"
	"testing"
)

func TestWidgetSeparatesOpenAndClosedWithoutMutatingState(t *testing.T) {
	phases := []tasks.Phase{{Name: "Build", Tasks: []tasks.Task{{Content: "finished", Status: "completed"}, {Content: "working", Status: "in_progress"}, {Content: "waiting", Status: "pending"}, {Content: "obsolete", Status: "abandoned"}}}}
	before := tasks.ClonePhases(phases)
	for _, width := range []int{20, 71, 72, 120} {
		lines := widgetLines(phases, width)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "working") || !strings.Contains(joined, "finished") {
			t.Fatalf("missing state at %d: %s", width, joined)
		}
		if width >= 72 {
			if !strings.Contains(lines[2], "working") || !strings.Contains(lines[2], "finished") || !strings.Contains(lines[2], " │ ") {
				t.Fatalf("columns: %v", lines)
			}
		} else if strings.Index(joined, "working") > strings.Index(joined, "finished") {
			t.Fatalf("closed task above open: %v", lines)
		}
		for _, line := range lines {
			if tui.Width(line) > width {
				t.Fatalf("overflow %d: %q", width, line)
			}
		}
	}
	if !reflect.DeepEqual(before, phases) {
		t.Fatal("display reordered task state")
	}
}

func TestWidgetRetainsCompletedPhaseAndSanitizes(t *testing.T) {
	if widgetLines(nil, 80) != nil {
		t.Fatal("empty widget visible")
	}
	phases := []tasks.Phase{{Name: "Done", Tasks: []tasks.Task{{Content: "finished\x1b[31m界", Status: "completed"}}}}
	for _, width := range []int{1, 10, 80} {
		for _, line := range widgetLines(phases, width) {
			if strings.Contains(line, "\x1b") || tui.Width(line) > width {
				t.Fatalf("unsafe layout: %q", line)
			}
		}
	}
	if !strings.Contains(strings.Join(widgetLines(phases, 80), "\n"), "finished") {
		t.Fatal("completed work disappeared")
	}
}
