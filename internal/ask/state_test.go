package ask

import (
	"github.com/VBenevides/pig-plugins/internal/tui"
	"reflect"
	"strings"
	"testing"
)

func sample(multi bool) Question {
	return Question{Question: "Pick a language?", Header: "Language", Options: []Option{{Label: "Go", Description: "Compiled", Preview: "package main\n"}, {Label: "Rust", Description: "Compiled"}}, Multi: multi}
}
func keys(t *testing.T, s *State, inputs ...string) *Result {
	t.Helper()
	var result *Result
	for _, input := range inputs {
		var err error
		result, err = s.Handle(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}
func TestSingleAndPreview(t *testing.T) {
	q := sample(false)
	result := keys(t, New(Params{Questions: []Question{q}}), "\r")
	want := Result{Answers: []Answer{{Index: 0, Question: q.Question, Kind: "option", Answer: new("Go"), Preview: q.Options[0].Preview}}}
	if result == nil || !reflect.DeepEqual(*result, want) {
		t.Fatalf("got %#v want %#v", result, want)
	}
}
func TestMultiOrderEmptyAndCustomReplacement(t *testing.T) {
	q := sample(true)
	for _, tt := range []struct {
		name   string
		inputs []string
		want   Answer
	}{
		{"ordered", []string{"\x1b[B", " ", "\x1b[A", " ", "\x1b[B", "\x1b[B", "\x1b[B", "\r"}, Answer{Index: 0, Question: q.Question, Kind: "multi", Selected: new([]string{"Go", "Rust"})}},
		{"empty commit", []string{"\x1b[A", "\r"}, Answer{Index: 0, Question: q.Question, Kind: "multi", Selected: new([]string{})}},
		{"custom replaces choices", []string{" ", "\x1b[B", "\x1b[B", "Other language", "\r"}, Answer{Index: 0, Question: q.Question, Kind: "custom", Answer: new("Other language")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := keys(t, New(Params{Questions: []Question{q}}), tt.inputs...)
			if result == nil || len(result.Answers) != 1 || !reflect.DeepEqual(result.Answers[0], tt.want) {
				t.Fatalf("got %#v want %#v", result, tt.want)
			}
		})
	}
}
func TestPartialAnswersAndNotes(t *testing.T) {
	q := sample(false)
	second := q
	second.Question = "Pick a version?"
	second.Header = "Version"
	s := New(Params{Questions: []Question{q, second}})
	result := keys(t, s, "n", " use stable ", "\x1b", "\r", "\t", "n", " ship now ", "\r", "\r")
	want := Result{Answers: []Answer{{Index: 0, Question: q.Question, Kind: "option", Answer: new("Go"), Preview: q.Options[0].Preview, Notes: "use stable"}}, GlobalNote: "ship now"}
	if result == nil || !reflect.DeepEqual(*result, want) {
		t.Fatalf("got %#v want %#v", result, want)
	}
}
func TestDraftEditingAndCancellation(t *testing.T) {
	s := New(Params{Questions: []Question{sample(false)}})
	result := keys(t, s, "\x1b[A", "A😀C", "\x1b[D", "\x7f", "界", "\x1b[13;2u", "\r")
	if result == nil || result.Answers[0].Answer == nil || *result.Answers[0].Answer != "A界\nC" {
		t.Fatalf("edited answer: %#v", result)
	}
	s = New(Params{Questions: []Question{sample(true)}})
	result = keys(t, s, " ", "\x1b")
	if result == nil || !result.Cancelled || len(result.Answers) != 1 || result.Answers[0].Kind != "multi" {
		t.Fatalf("cancel must retain partial answers: %#v", result)
	}
}
func TestCollapseAndReleasedKeysDoNotChangeAnswers(t *testing.T) {
	s := New(Params{Questions: []Question{sample(true)}})
	result := keys(t, s, "\x1d", " ", "\r", "\x1b[93;5:2u", "\x1b[93;5:3u")
	if result != nil || !s.collapsed || len(s.answers) != 0 {
		t.Fatalf("collapsed state mutated: %#v", s)
	}
	result = keys(t, s, "\x1d", "\x1b[32;1:3u", " ", "\x1b")
	if result == nil || !result.Cancelled || len(result.Answers) != 1 {
		t.Fatalf("expand/cancel: %#v", result)
	}
}
func TestRenderBoundsAndTerminalControls(t *testing.T) {
	q := sample(false)
	q.Question = "界😀\x1b]52;c;clipboard\a"
	q.Options[0].Preview = strings.Repeat("界", 120) + "\nline\n\x1b[31mcolor"
	s := New(Params{Questions: []Question{q}})
	for _, width := range []int{1, 10, 80, 120} {
		for _, line := range s.Render(width) {
			if tui.Width(line) > width || strings.ContainsAny(line, "\x1b\a") {
				t.Fatalf("unsafe/overwide row at %d: %q", width, line)
			}
		}
	}
}
