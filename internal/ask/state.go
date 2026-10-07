package ask

import (
	"github.com/VBenevides/pig-plugins/internal/tui"
	"strings"
)

type State struct {
	Params          Params
	tab, row        int
	answers         map[int]Answer
	checked         map[int][]bool
	drafts          map[int]*tui.Editor
	notes           map[int]string
	noteMode        bool
	noteEditor      tui.Editor
	collapsed       bool
	CollapseKey     string
	EditorRequested bool
}

func New(p Params) *State {
	return &State{Params: p, answers: map[int]Answer{}, checked: map[int][]bool{}, drafts: map[int]*tui.Editor{}, notes: map[int]string{}, CollapseKey: "ctrl+]"}
}
func (s *State) tabs() int {
	if len(s.Params.Questions) > 1 {
		return len(s.Params.Questions) + 1
	}
	return 1
}
func (s *State) review() bool       { return s.tab == len(s.Params.Questions) }
func (s *State) question() Question { return s.Params.Questions[s.tab] }
func (s *State) input() bool        { return !s.review() && s.row == len(s.question().Options) }
func (s *State) draft() *tui.Editor {
	editor := s.drafts[s.tab]
	if editor == nil {
		editor = &tui.Editor{}
		s.drafts[s.tab] = editor
	}
	return editor
}
func (s *State) CustomText() string            { return s.draft().Text }
func (s *State) ReplaceCustomText(text string) { s.draft().Set(text) }
func (s *State) rows() int {
	if s.review() {
		return 2
	}
	n := len(s.question().Options) + 1
	if s.question().Multi {
		n++
	}
	return n
}
func (s *State) result(cancelled bool) *Result {
	r := Result{Answers: []Answer{}, Cancelled: cancelled, GlobalNote: s.notes[len(s.Params.Questions)]}
	for i := range s.Params.Questions {
		if a, ok := s.answers[i]; ok {
			r.Answers = append(r.Answers, a)
		}
	}
	return &r
}
func (s *State) advance() *Result {
	if s.tabs() == 1 {
		return s.result(false)
	}
	s.tab++
	s.row = 0
	return nil
}
func (s *State) selection() []string {
	q := s.question()
	selected := []string{}
	for i, value := range s.checked[s.tab] {
		if value {
			selected = append(selected, q.Options[i].Label)
		}
	}
	return selected
}
func (s *State) storeMulti(committed bool) {
	selected := s.selection()
	if len(selected) == 0 && !committed {
		delete(s.answers, s.tab)
		return
	}
	s.answers[s.tab] = Answer{Index: s.tab, Question: s.question().Question, Kind: "multi", Selected: new(selected), Notes: s.notes[s.tab]}
}
func (s *State) toggle() {
	q := s.question()
	checked := s.checked[s.tab]
	if checked == nil {
		checked = make([]bool, len(q.Options))
		s.checked[s.tab] = checked
	}
	checked[s.row] = !checked[s.row]
	s.storeMulti(false)
}
func (s *State) commitNote() {
	note := strings.TrimSpace(s.noteEditor.Text)
	s.notes[s.tab] = note
	if a, ok := s.answers[s.tab]; ok {
		a.Notes = note
		s.answers[s.tab] = a
	}
	s.noteMode = false
}
func (s *State) moveRow(direction int) { s.row = (s.row + direction + s.rows()) % s.rows() }

// Handle returns a result only when the questionnaire is submitted or cancelled.
func (s *State) Handle(data string) (*Result, error) {
	if tui.Released(data) {
		return nil, nil
	}
	key := tui.Key(data)
	if tui.Matches(data, s.CollapseKey) {
		if !tui.Repeated(data) {
			s.collapsed = !s.collapsed
		}
		return nil, nil
	}
	if s.collapsed {
		if key == "escape" {
			return s.result(true), nil
		}
		return nil, nil
	}
	if s.noteMode {
		if key == "escape" || key == "enter" {
			s.commitNote()
			return nil, nil
		}
		s.noteEditor.Handle(data)
		return nil, nil
	}
	if key == "escape" {
		return s.result(true), nil
	}
	if s.input() {
		editor := s.draft()
		switch key {
		case "ctrl+g":
			s.EditorRequested = true
			return nil, nil
		case "enter":
			q := s.question()
			var answer *string
			if editor.Text != "" {
				answer = new(editor.Text)
			}
			s.answers[s.tab] = Answer{Index: s.tab, Question: q.Question, Kind: "custom", Answer: answer, Notes: s.notes[s.tab]}
			delete(s.checked, s.tab)
			return s.advance(), nil
		case "up":
			if !editor.Vertical(-1) {
				s.moveRow(-1)
			}
			return nil, nil
		case "down":
			if !editor.Vertical(1) {
				s.moveRow(1)
			}
			return nil, nil
		}
		editor.Handle(data)
		return nil, nil
	}
	if s.tabs() > 1 && (key == "tab" || key == "shift+tab" || key == "left" || key == "right") {
		direction := 1
		if key == "shift+tab" || key == "left" {
			direction = -1
		}
		s.tab = (s.tab + direction + s.tabs()) % s.tabs()
		s.row = 0
		return nil, nil
	}
	if data == "n" {
		s.noteMode = true
		s.noteEditor.Set(s.notes[s.tab])
		return nil, nil
	}
	if key == "up" || key == "down" {
		direction := 1
		if key == "up" {
			direction = -1
		}
		s.moveRow(direction)
		return nil, nil
	}
	if s.review() {
		if key == "enter" {
			return s.result(s.row == 1), nil
		}
		return nil, nil
	}
	q := s.question()
	if q.Multi {
		if s.row < len(q.Options) && (key == "space" || key == "enter") {
			s.toggle()
		} else if s.row == len(q.Options)+1 && key == "enter" {
			s.storeMulti(true)
			return s.advance(), nil
		}
		return nil, nil
	}
	if key == "enter" && s.row < len(q.Options) {
		o := q.Options[s.row]
		s.answers[s.tab] = Answer{Index: s.tab, Question: q.Question, Kind: "option", Answer: new(o.Label), Preview: o.Preview, Notes: s.notes[s.tab]}
		return s.advance(), nil
	}
	return nil, nil
}
