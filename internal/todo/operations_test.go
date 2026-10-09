package todo

import (
	"reflect"
	"strings"
	"testing"
)

func TestPhasedOperations(t *testing.T) {
	phases, errors := ApplyOperation(nil, Operation{Op: "init", List: []PhaseInput{{"Foundation", []string{"Build core", "Add edges"}}, {"Verification", []string{"Run tests", "Write docs"}}}})
	if len(errors) > 0 || phases[0].Tasks[0].Status != "in_progress" {
		t.Fatalf("init: %#v %v", phases, errors)
	}
	steps := []struct {
		op       Operation
		statuses []string
	}{
		{Operation{Op: "start", Task: "Run tests"}, []string{"pending", "pending", "in_progress", "pending"}},
		{Operation{Op: "done", Phase: "Verification"}, []string{"in_progress", "pending", "completed", "completed"}},
		{Operation{Op: "done", Task: "Build core"}, []string{"completed", "in_progress", "completed", "completed"}},
		{Operation{Op: "drop", Task: "Add edges"}, []string{"completed", "abandoned", "completed", "completed"}},
	}
	for _, step := range steps {
		before := ClonePhases(phases)
		next, errs := ApplyOperation(phases, step.op)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		if !reflect.DeepEqual(before, phases) {
			t.Fatal("modified input")
		}
		got := []string{}
		for _, p := range next {
			for _, task := range p.Tasks {
				got = append(got, task.Status)
			}
		}
		if !reflect.DeepEqual(got, step.statuses) {
			t.Fatalf("%s: %v", step.op.Op, got)
		}
		phases = next
	}
	phases, errors = ApplyOperation(phases, Operation{Op: "append", Phase: "Follow-up", Items: []string{"New work"}})
	if len(errors) > 0 || phases[2].Tasks[0].Status != "in_progress" {
		t.Fatal(phases, errors)
	}
	for _, op := range []Operation{{Op: "rm", Task: "New work"}, {Op: "rm", Phase: "Verification"}, {Op: "rm"}} {
		phases, errors = ApplyOperation(phases, op)
		if len(errors) > 0 {
			t.Fatal(errors)
		}
	}
	if len(phases) != 3 {
		t.Fatal("removed phase containers")
	}
	for _, p := range phases {
		if len(p.Tasks) > 0 {
			t.Fatal(phases)
		}
	}
}
func TestPhasedRejectionsAreAtomic(t *testing.T) {
	current, _ := ApplyOperation(nil, Operation{Op: "init", Items: []string{"Keep"}})
	cases := []struct {
		op      Operation
		message string
	}{
		{Operation{Op: "init"}, "Missing list"},
		{Operation{Op: "init", List: []PhaseInput{{"P", []string{"one"}}, {"P", []string{"two"}}}}, "Duplicate phase"},
		{Operation{Op: "init", List: []PhaseInput{{"P", []string{"same"}}, {"Q", []string{"same"}}}}, "Duplicate task"},
		{Operation{Op: "init", List: []PhaseInput{{"P", []string{}}}}, "has no tasks"},
		{Operation{Op: "start"}, "Missing task"},
		{Operation{Op: "done", Task: "task-1"}, "referenced by content"},
		{Operation{Op: "drop", Phase: "unknown"}, "not found"},
		{Operation{Op: "rm", Task: "unknown"}, "not found"},
		{Operation{Op: "append", Items: []string{"x"}}, "Missing phase"},
		{Operation{Op: "append", Phase: "P"}, "Missing items"},
		{Operation{Op: "append", Phase: "P", Items: []string{"Keep"}}, "already exists"},
		{Operation{Op: "append", Phase: "P", Items: []string{"new", "new"}}, "already exists"},
		{Operation{Op: "bad"}, "Unknown operation"},
	}
	for _, tc := range cases {
		t.Run(tc.message, func(t *testing.T) {
			before := ClonePhases(current)
			next, errors := ApplyOperation(current, tc.op)
			if len(errors) == 0 || !strings.Contains(strings.Join(errors, "; "), tc.message) {
				t.Fatal(errors)
			}
			if !reflect.DeepEqual(next, before) || !reflect.DeepEqual(current, before) {
				t.Fatal("rejection changed state")
			}
		})
	}
}
func TestViewNormalizationAndTransitions(t *testing.T) {
	current := []Phase{{"P", []Task{{"one", "in_progress"}, {"two", "in_progress"}}}}
	view, errors := ApplyOperation(current, Operation{Op: "view"})
	if len(errors) > 0 || !reflect.DeepEqual(view, current) {
		t.Fatal(view, errors)
	}
	Normalize(view)
	if view[0].Tasks[1].Status != "pending" || current[0].Tasks[1].Status != "in_progress" {
		t.Fatal("normalization/clone")
	}
	next, errors := ApplyOperation(view, Operation{Op: "done", Task: "one"})
	if len(errors) > 0 {
		t.Fatal(errors)
	}
	if got := CompletionTransitions(view, next); !reflect.DeepEqual(got, []Completion{{"P", "one"}}) {
		t.Fatal(got)
	}
	again, _ := ApplyOperation(next, Operation{Op: "done", Task: "one"})
	if len(CompletionTransitions(next, again)) != 0 {
		t.Fatal("duplicate transition")
	}
	empty, errors := ApplyOperation(current, Operation{Op: "init", List: []PhaseInput{}})
	if len(errors) > 0 || len(empty) != 0 {
		t.Fatal(empty, errors)
	}
}
