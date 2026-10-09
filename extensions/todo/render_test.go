package todoext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Ignore only trusted SGR styling; all other terminal controls remain forbidden.
func TestViewerBoundsAndControlFiltering(t *testing.T) {
	v := &viewer{phases: []tasks.Phase{{Name: "界", Tasks: []tasks.Task{{Content: "界😀\x1b]52;c;clipboard\a\u009b2J", Status: "pending"}, {Content: strings.Repeat("界", 100), Status: "completed"}}}}}
	for _, width := range []int{1, 8, 80} {
		for _, line := range v.Render(width) {
			line = sgr.ReplaceAllString(line, "")
			if tui.Width(line) > width || strings.ContainsAny(line, "\x1b\a\u009b") {
				t.Fatalf("unsafe or overwide viewer row at %d: %q", width, line)
			}
		}
	}
}

func TestOperationCallLabels(t *testing.T) {
	cases := []struct {
		op   tasks.Operation
		want string
	}{
		{tasks.Operation{Op: "init", Items: []string{"a", "b"}}, "todo init (1 phase, 2 tasks)"},
		{tasks.Operation{Op: "init", List: []tasks.PhaseInput{{Phase: "P", Items: []string{"a"}}, {Phase: "Q", Items: []string{"b"}}}}, "todo init (2 phases, 2 tasks)"},
		{tasks.Operation{Op: "append", Phase: "P", Items: []string{"a"}}, "todo append: P (1 item)"},
		{tasks.Operation{Op: "start", Task: "a"}, "todo start: a"},
		{tasks.Operation{Op: "drop", Phase: "P"}, "todo drop: P"},
		{tasks.Operation{Op: "rm"}, "todo rm: all"},
		{tasks.Operation{Op: "view"}, "todo view"},
	}
	for _, tc := range cases {
		if got := callLabel(tc.op); got != tc.want {
			t.Fatalf("%s != %s", got, tc.want)
		}
	}
	if roman(4) != "IV" || roman(19) != "XIX" || roman(0) != "" {
		t.Fatal("Roman labels")
	}
}

func TestCollapsedAndExpandedPhases(t *testing.T) {
	phases := []tasks.Phase{{Name: "Earlier", Tasks: []tasks.Task{{Content: "earlier", Status: "completed"}}}, {Name: "Active", Tasks: []tasks.Task{{Content: "working", Status: "in_progress"}}}, {Name: "Later", Tasks: []tasks.Task{{Content: "later", Status: "pending"}}}}
	collapsed := strings.Join(phaseLines(phases, nil, false, tasks.Operation{Op: "view"}, sdk.UITheme{}, 80), "\n")
	if !strings.Contains(collapsed, "I. Earlier — 1/1 done") || !strings.Contains(collapsed, "[•] working") || strings.Contains(collapsed, "[ ] later") {
		t.Fatal(collapsed)
	}
	expanded := strings.Join(phaseLines(phases, nil, true, tasks.Operation{Op: "view"}, sdk.UITheme{}, 80), "\n")
	if !strings.Contains(expanded, "[✓] earlier") || !strings.Contains(expanded, "[ ] later") {
		t.Fatal(expanded)
	}
	touched := strings.Join(phaseLines(phases, []tasks.Completion{{Phase: "Earlier", Content: "earlier"}}, false, tasks.Operation{Op: "done", Phase: "Earlier"}, sdk.UITheme{}, 80), "\n")
	if !strings.Contains(touched, "[✓] earlier") {
		t.Fatal(touched)
	}
}

func TestResultFallbackAndViewerClosure(t *testing.T) {
	result := sdk.ToolRenderResult{Content: []map[string]any{{"type": "text", "text": "Errors: missing"}}, Details: tasks.PhasedDetails{Phases: []tasks.Phase{{Name: "P", Tasks: []tasks.Task{{Content: "must not render", Status: "pending"}}}}}}
	lines, err := resultLines(result, sdk.ToolRenderResultOptions{}, sdk.ToolRenderContext{IsError: true}, sdk.UITheme{}, 80)
	if err != nil || !reflect.DeepEqual(lines, []string{"Errors: missing"}) {
		t.Fatal(lines, err)
	}
	v := viewer{}
	for _, key := range []string{"\x1b", "\x03"} {
		result, err := v.HandleInput(key)
		if err != nil || !result.Done {
			t.Fatal(key, result, err)
		}
	}
}
