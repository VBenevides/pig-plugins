package todo

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPinnedUpstreamParity(t *testing.T) {
	var fixture struct {
		Commit     string
		Schema     map[string]any
		Operations []struct {
			Params    Operation
			Phases    []Phase
			Errors    []string
			Completed []Completion
			Summary   string
			Widget    []string
		}
		Branches []struct {
			Branch []map[string]any
			Phases []Phase
		}
		Markdown []struct {
			Text   string
			Phases []Phase
			Errors []string
			Export string
		}
	}
	data, err := os.ReadFile("../../testfixtures/todotools.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Commit != "50b85f7e39c94a8fa8253eb3628515f188a42a1c" || len(fixture.Operations) != 35 {
		t.Fatal("wrong/empty fixture")
	}
	if !reflect.DeepEqual(PhasedSchema(), fixture.Schema) {
		t.Fatalf("schema differs: %#v != %#v", PhasedSchema(), fixture.Schema)
	}
	phases := []Phase{}
	for i, step := range fixture.Operations {
		next, errors := ApplyOperation(phases, step.Params)
		if !reflect.DeepEqual(next, step.Phases) || !reflect.DeepEqual(errors, step.Errors) {
			t.Fatalf("step %d %v: %#v %v != %#v %v", i, step.Params, next, errors, step.Phases, step.Errors)
		}
		completed := []Completion{}
		if step.Params.Op != "view" && len(errors) == 0 {
			completed = CompletionTransitions(phases, next)
		}
		if !reflect.DeepEqual(completed, step.Completed) {
			t.Fatalf("step %d transitions: %v != %v", i, completed, step.Completed)
		}
		if summary := FormatSummary(next, errors, step.Params.Op == "view"); summary != step.Summary {
			t.Fatalf("step %d summary: %q != %q", i, summary, step.Summary)
		}
		if widget := WidgetLines(next); !reflect.DeepEqual(widget, step.Widget) {
			t.Fatalf("step %d widget: %v != %v", i, widget, step.Widget)
		}
		phases = next
	}
	for i, step := range fixture.Branches {
		phases, _ := Restore(step.Branch)
		if !reflect.DeepEqual(phases, step.Phases) {
			t.Fatalf("branch %d: %v != %v", i, phases, step.Phases)
		}
	}
	for i, step := range fixture.Markdown {
		phases, errors := MarkdownToPhases(step.Text)
		if !reflect.DeepEqual(phases, step.Phases) || !reflect.DeepEqual(errors, step.Errors) || PhasesToMarkdown(phases) != step.Export {
			t.Fatalf("markdown %d: %v %v", i, phases, errors)
		}
	}
}
