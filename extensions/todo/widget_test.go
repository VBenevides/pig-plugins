package todoext

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tasks "github.com/VBenevides/pig-plugins/internal/todo"
)

func TestWidgetKeepsAllTaskRowsWithoutMutatingState(t *testing.T) {
	phases := []tasks.Phase{
		{Name: "Build", Tasks: []tasks.Task{{Content: "finished", Status: "completed"}, {Content: "working", Status: "in_progress"}}},
		{Name: "Check", Tasks: []tasks.Task{{Content: "waiting", Status: "pending"}, {Content: "obsolete", Status: "abandoned"}}},
	}
	before := tasks.ClonePhases(phases)
	for _, width := range []int{1, 20, 71, 72, 120} {
		lines := widgetLines(phases, width)
		if len(lines) != 5 || !strings.Contains(lines[0], "2/4") {
			t.Fatalf("missing task rows: %v", lines)
		}
		for i, want := range []string{"[✓] Build · finished", "[•] Build · working", "[ ] Check · waiting", "[×] Check · obsolete"} {
			if lines[i+1] != want {
				t.Fatalf("row %d: %q, want %q", i, lines[i+1], want)
			}
		}
	}
	if !reflect.DeepEqual(before, phases) {
		t.Fatal("display changed task state")
	}
}

func TestWidgetRetainsCompletedPhaseAndSanitizes(t *testing.T) {
	if widgetLines(nil, 80) != nil || widgetLines([]tasks.Phase{{Name: "empty"}}, 80) != nil {
		t.Fatal("empty widget visible")
	}
	phases := []tasks.Phase{{Name: "Done\x1b[31m", Tasks: []tasks.Task{{Content: "finished\x1b[31m界\nnext", Status: "completed"}}}}
	lines := widgetLines(phases, 1)
	if len(lines) != 2 || !strings.Contains(lines[1], "finished界 next") {
		t.Fatalf("completed work disappeared: %v", lines)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\x1b\n") {
			t.Fatalf("unsafe row: %q", line)
		}
	}
}

func TestWidgetKeepsTasksBeyondOldTruncationLimit(t *testing.T) {
	phase := tasks.Phase{Name: "Many"}
	for i := 0; i < 30; i++ {
		phase.Tasks = append(phase.Tasks, tasks.Task{Content: fmt.Sprintf("task %d", i), Status: "pending"})
	}
	lines := widgetLines([]tasks.Phase{phase}, 80)
	if len(lines) != 31 || !strings.Contains(lines[30], "task 29") {
		t.Fatalf("tasks truncated: %v", lines)
	}
}
