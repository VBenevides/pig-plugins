package todo

import (
	"reflect"
	"strings"
	"testing"
)

func TestWidgetActivePhaseAndTerminalHide(t *testing.T) {
	phases := []Phase{{"Earlier", []Task{{"pending first", "pending"}}}, {"Active", []Task{{"working", "in_progress"}, {"done", "completed"}, {"dropped", "abandoned"}}}}
	want := []string{"Todo", "Active", "[•] working", "[✓] done", "[×] dropped"}
	if got := WidgetLines(phases); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	closed, _ := ApplyOperation(phases, Operation{Op: "done"})
	if WidgetLines(closed) != nil || WidgetLines(nil) != nil {
		t.Fatal("terminal widget not hidden")
	}
	earlier, _ := ApplyOperation(phases, Operation{Op: "done", Phase: "Active"})
	if WidgetLines(earlier)[1] != "Earlier" {
		t.Fatal(earlier)
	}
}
func TestSanitizeText(t *testing.T) {
	for _, text := range []string{" \x1b[31mred\x1b[0m\n\t text ", "\x1b]52;c;clipboard\a red\rtext", "\u009b2Jred\x00text"} {
		if got := SanitizeText(text); got != "red text" {
			t.Fatalf("%q => %q", text, got)
		}
	}
}
func TestMarkdownRoundTrip(t *testing.T) {
	original := []Phase{{"Build", []Task{{"work", "in_progress"}, {"wait", "pending"}, {"done", "completed"}, {"drop", "abandoned"}}}, {"Empty", []Task{}}}
	text := PhasesToMarkdown(original)
	got, errors := MarkdownToPhases(text)
	if len(errors) > 0 || !reflect.DeepEqual(got, original) {
		t.Fatal(text, got, errors)
	}
	parsed, errors := MarkdownToPhases("- [X] closed\r\n+ [>] active\n* [~] gone\n- [?] invalid\nbad syntax")
	if len(errors) != 2 || len(parsed) != 1 || parsed[0].Name != "Tasks" || parsed[0].Tasks[1].Status != "in_progress" {
		t.Fatal(parsed, errors)
	}
	if !strings.Contains(errors[0], "Line 4") || !strings.Contains(errors[1], "Line 5") {
		t.Fatal(errors)
	}
	if PhasesToMarkdown(nil) != "# Tasks\n" || ResolveMarkdownPath(" 'tasks.md' ", "/work") != "/work/tasks.md" || ResolveMarkdownPath("", "/work") != "/work/TODO.md" {
		t.Fatal("markdown defaults")
	}
}
func TestSummaryText(t *testing.T) {
	phases, _ := ApplyOperation(nil, Operation{Op: "init", Items: []string{"first", "second"}})
	want := "Remaining items (2):\n  - first [in_progress] (Tasks)\n  - second [pending] (Tasks)\nOverall: 0/2 done, 2 open.\nActive phase 1/1 \"Tasks\" (0/2).\n  Tasks:\n    - [ ] first (in progress)\n    - [ ] second"
	if got := FormatSummary(phases, nil, false); got != want {
		t.Fatalf("%q", got)
	}
	if FormatSummary(nil, nil, true) != "Todo list is empty." || FormatSummary(nil, nil, false) != "Todo list cleared." || FormatSummary(nil, []string{"bad"}, false) != "Errors: bad" {
		t.Fatal("empty summaries")
	}
}
