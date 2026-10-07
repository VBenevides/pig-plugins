// Package ask implements the rpiv structured questionnaire contract and state machine.
package ask

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
)

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Preview     string `json:"preview,omitempty"`
}
type Question struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []Option `json:"options"`
	Multi    bool     `json:"multiSelect,omitempty"`
}
type Params struct {
	Questions []Question `json:"questions"`
}
type Answer struct {
	Index    int       `json:"questionIndex"`
	Question string    `json:"question"`
	Kind     string    `json:"kind"`
	Answer   *string   `json:"answer"`
	Selected *[]string `json:"selected,omitempty"`
	Notes    string    `json:"notes,omitempty"`
	Preview  string    `json:"preview,omitempty"`
}
type Result struct {
	Answers    []Answer `json:"answers"`
	Cancelled  bool     `json:"cancelled"`
	GlobalNote string   `json:"globalNote,omitempty"`
	Error      string   `json:"error,omitempty"`
}
type ValidationError struct{ Code, Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(code, message string) error { return &ValidationError{code, message} }
func normalized(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "")
}
func codeUnits(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
func Parse(raw map[string]any) (Params, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return Params{}, err
	}
	var p Params
	if err = json.Unmarshal(data, &p); err != nil {
		return Params{}, err
	}
	if len(p.Questions) == 0 {
		return p, invalid("no_questions", "At least one question is required")
	}
	if len(p.Questions) > 4 {
		return p, invalid("too_many_questions", "At most 4 questions are allowed per invocation")
	}
	// Normalize before validation, and check all question texts before option errors.
	seen := map[string]bool{}
	for i := range p.Questions {
		q := &p.Questions[i]
		q.Question = normalized(q.Question)
		q.Header = normalized(q.Header)
		for j := range q.Options {
			o := &q.Options[j]
			o.Label = normalized(o.Label)
			o.Description = normalized(o.Description)
			o.Preview = normalized(o.Preview)
		}
		if seen[q.Question] {
			return p, invalid("duplicate_question", "Question text must be unique within an invocation")
		}
		seen[q.Question] = true
	}
	for _, q := range p.Questions {
		if codeUnits(q.Header) > 16 {
			return p, fmt.Errorf("question header exceeds 16 characters")
		}
		if len(q.Options) < 2 {
			return p, invalid("empty_options", "Each question requires at least 2 options")
		}
		if len(q.Options) > 4 {
			return p, fmt.Errorf("each question permits at most 4 options")
		}
		labels := map[string]bool{}
		for _, o := range q.Options {
			if codeUnits(o.Label) > 60 {
				return p, fmt.Errorf("option label exceeds 60 characters")
			}
			if slices.Contains([]string{"Other", "Type something.", "Next"}, o.Label) {
				return p, invalid("reserved_label", "Option label is reserved (Other, Type something., Next)")
			}
			if labels[o.Label] {
				return p, invalid("duplicate_option_label", "Option labels must be unique within a question")
			}
			labels[o.Label] = true
		}
	}
	return p, nil
}
func Empty(code string) Result { return Result{Answers: []Answer{}, Cancelled: true, Error: code} }
func Scalar(a Answer) string {
	if a.Kind == "multi" {
		if a.Selected != nil && len(*a.Selected) > 0 {
			return strings.Join(*a.Selected, ", ")
		}
		return "(no input)"
	}
	if a.Answer != nil && (a.Kind == "option" || *a.Answer != "") {
		return *a.Answer
	}
	return "(no input)"
}
func Envelope(result Result, p Params) (string, Result) {
	if result.Answers == nil {
		result.Answers = []Answer{}
	}
	if result.Cancelled {
		return "User declined to answer questions", result
	}
	var segments []string
	for i := range p.Questions {
		for _, a := range result.Answers {
			if a.Index != i {
				continue
			}
			text := fmt.Sprintf("\"%s\"=\"%s\"", a.Question, Scalar(a))
			if a.Preview != "" {
				text += ". selected preview: " + a.Preview
			}
			if a.Notes != "" {
				text += ". user notes: " + a.Notes
			}
			segments = append(segments, text+".")
			break
		}
	}
	if result.GlobalNote != "" {
		segments = append(segments, "global note: "+result.GlobalNote+".")
	}
	if len(segments) == 0 {
		result.Cancelled = true
		return "User declined to answer questions", result
	}
	return "User has answered your questions: " + strings.Join(segments, " ") + " You can now continue with the user's answers in mind.", result
}
