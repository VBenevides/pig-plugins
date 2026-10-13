package todoext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	tasks "github.com/VBenevides/pig-plugins/internal/todo"
	"github.com/VBenevides/pig-plugins/internal/tui"
	"reflect"
	"regexp"
	"strconv"
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

func manyTasks(n int) []tasks.Phase {
	phase := tasks.Phase{Name: "Big"}
	for i := 1; i <= n; i++ {
		phase.Tasks = append(phase.Tasks, tasks.Task{Content: "task-" + strconv.Itoa(i), Status: "pending"})
	}
	return []tasks.Phase{phase}
}

func plain(lines []string) string { return sgr.ReplaceAllString(strings.Join(lines, "\n"), "") }

func TestViewerScrollsToEveryItemWithArrowsAndPageKeys(t *testing.T) {
	const n, rows = 40, 6
	v := &viewer{phases: manyTasks(n), rows: rows}
	first := plain(v.Render(80))
	if !strings.Contains(first, "task-1") || strings.Contains(first, "task-40") || !strings.Contains(first, "of ") {
		t.Fatalf("the first page must show the start and a position, not everything:\n%s", first)
	}
	if got := len(v.Render(80)); got > rows+6 {
		t.Fatalf("render is %d rows, want at most %d", got, rows+6)
	}
	press := func(keys ...string) string {
		t.Helper()
		for _, key := range keys {
			if result, err := v.HandleInput(key); err != nil || result.Done {
				t.Fatalf("key %q: done=%v err=%v", key, result.Done, err)
			}
		}
		return plain(v.Render(80))
	}
	seen := map[string]bool{}
	collect := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			if i := strings.Index(line, "task-"); i >= 0 {
				seen[strings.Fields(line[i:])[0]] = true
			}
		}
	}
	collect(first)
	for page := 0; page < n; page++ {
		collect(press("\x1b[6~"))
	}
	for i := 1; i <= n; i++ {
		if !seen["task-"+strconv.Itoa(i)] {
			t.Errorf("task-%d was never reachable with PgDn", i)
		}
	}
	if end := press("\x1b[F"); !strings.Contains(end, "task-40") || strings.Contains(end, "task-1\n") {
		t.Fatalf("End must show the last page:\n%s", end)
	}
	if past := press("\x1b[B", "\x1b[B", "\x1b[6~"); !strings.Contains(past, "task-40") {
		t.Fatalf("scrolling past the end must clamp to the last page:\n%s", past)
	}
	if start := press("\x1b[5~", "\x1b[H"); !strings.Contains(start, "task-1") || strings.Contains(start, "task-7") {
		t.Fatalf("Home must show the first page:\n%s", start)
	}
	if before := press("\x1b[A", "\x1b[5~"); !strings.Contains(before, "task-1") {
		t.Fatalf("scrolling above the start must clamp to the first page:\n%s", before)
	}
	one := press("\x1b[B")
	// The list starts with the phase header, so one row down drops the header and reveals one more task.
	if !strings.Contains(one, "rows 2-7 of 41") || !strings.Contains(one, "task-6") {
		t.Fatalf("Down moves exactly one row:\n%s", one)
	}
	if again := press("j", "k"); again != one {
		t.Fatalf("j and k scroll like Down and Up")
	}
}

func TestViewerClosesWithEscapeQAndCtrlCAndIgnoresReleases(t *testing.T) {
	for _, key := range []string{"\x1b", "q", "\x03"} {
		v := &viewer{phases: manyTasks(30), rows: 5}
		if result, err := v.HandleInput(key); err != nil || !result.Done {
			t.Errorf("key %q must close the viewer: %+v %v", key, result, err)
		}
	}
	v := &viewer{phases: manyTasks(30), rows: 5}
	for _, key := range []string{"\x1b[113;1:3u", "\x1b[27;1:3u"} { // Key-release events for q and Escape.
		if result, err := v.HandleInput(key); err != nil || result.Done {
			t.Errorf("a key release must not close the viewer: %q", key)
		}
	}
}

func TestViewerWindowFollowsTheTerminalAndStaysBounded(t *testing.T) {
	v := &viewer{phases: manyTasks(100), terminalRows: func() int { return 30 }}
	lines := v.Render(60)
	if want := 30*7/10 - 4 + 5; len(lines) != want { // Window plus title, count, position, blank, hint.
		t.Fatalf("30-row terminal rendered %d rows, want %d", len(lines), want)
	}
	for _, height := range []int{0, 1, 5} {
		v := &viewer{phases: manyTasks(100), terminalRows: func() int { return height }}
		got := len(v.Render(60))
		if height == 0 && got != defaultViewerRows+5 {
			t.Errorf("unknown height must use the default window, got %d rows", got)
		}
		if height > 0 && got != 3+5 {
			t.Errorf("tiny terminal %d must keep the 3-row minimum, got %d rows", height, got)
		}
	}
	short := &viewer{phases: manyTasks(3), rows: 10}
	if out := plain(short.Render(60)); strings.Contains(out, "rows ") || !strings.Contains(out, "Esc or q close") {
		t.Fatalf("a list that fits needs no position row:\n%s", out)
	}
	for _, line := range v.Render(7) {
		if tui.Width(sgr.ReplaceAllString(line, "")) > 7 {
			t.Fatalf("overwide row: %q", line)
		}
	}
}
