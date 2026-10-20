package goalext

import (
	"errors"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/goal"
)

type resumeProbe struct {
	idle, pending                       bool
	idleErr, queueErr, saveErr, sendErr error
	entries                             []goal.State
	messages                            []any
	notices                             []string
}

func (p *resumeProbe) SetStatus(string, string)          {}
func (p *resumeProbe) Notify(text, _ string)             { p.notices = append(p.notices, text) }
func (p *resumeProbe) IsIdle() (bool, error)             { return p.idle, p.idleErr }
func (p *resumeProbe) HasPendingMessages() (bool, error) { return p.pending, p.queueErr }
func (p *resumeProbe) AppendEntry(_ string, data any) error {
	if p.saveErr != nil {
		return p.saveErr
	}
	p.entries = append(p.entries, data.(goal.State))
	return nil
}
func (p *resumeProbe) SendUserMessage(content any, deliverAs string) error {
	if deliverAs != "followUp" {
		panic(deliverAs)
	}
	if p.sendErr != nil {
		return p.sendErr
	}
	p.messages = append(p.messages, content)
	return nil
}

func TestResumeRetainsObjectiveAndProgressWithoutDuplicateContinuation(t *testing.T) {
	for _, outcome := range []goal.Outcome{goal.Stopped, goal.Blocked, ""} {
		t.Run(string(outcome), func(t *testing.T) {
			original := &goal.State{Objective: "finish existing work", Status: goal.Finished, Outcome: outcome, Summary: "two checks passed"}
			if outcome == "" {
				original.Status = goal.Working
			}
			snapshot := *original
			x := &extension{state: original, idleRuns: maxIdleRuns, toolCalls: 2, lastStop: "aborted"}
			p := &resumeProbe{idle: true}
			if err := x.resume(p); err != nil {
				t.Fatal(err)
			}
			if err := x.resume(p); err != nil {
				t.Fatal(err)
			}
			if len(p.messages) != 1 || p.messages[0] != goal.ContinueMessage || len(p.entries) != 1 {
				t.Fatalf("messages=%v entries=%v", p.messages, p.entries)
			}
			if x.state.Objective != original.Objective || x.state.Summary != original.Summary || x.state.Status != goal.Working || x.state.Outcome != "" {
				t.Fatalf("state=%+v", x.state)
			}
			if x.idleRuns != 0 || x.toolCalls != 0 || x.lastStop != "" {
				t.Fatal("old run counters retained")
			}
			if *original != snapshot {
				t.Fatal("resume mutated the historical goal state")
			}
			restored, warnings := goal.Restore([]map[string]any{{"type": "custom", "customType": goal.EntryType, "data": p.entries[0]}})
			if len(warnings) != 0 || *restored != *x.state {
				t.Fatalf("restored=%+v warnings=%v", restored, warnings)
			}
		})
	}
}

func TestResumeDoesNotRestartMissingCompletedBusyOrQueuedGoals(t *testing.T) {
	for _, tc := range []struct {
		name          string
		state         *goal.State
		idle, pending bool
	}{
		{"missing", nil, true, false},
		{"reached", &goal.State{Objective: "done", Status: goal.Finished, Outcome: goal.Reached}, true, false},
		{"busy", &goal.State{Objective: "work", Status: goal.Working}, false, false},
		{"queued", &goal.State{Objective: "work", Status: goal.Working}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := &extension{state: tc.state}
			p := &resumeProbe{idle: tc.idle, pending: tc.pending}
			if err := x.resume(p); err != nil {
				t.Fatal(err)
			}
			if len(p.messages) != 0 || len(p.entries) != 0 || x.state != tc.state || x.resumePending || len(p.notices) != 1 {
				t.Fatalf("unexpected continuation: %+v", p)
			}
		})
	}
}

func TestResumeFailuresRemainObservableAndRetryable(t *testing.T) {
	for _, stage := range []string{"idle", "queue", "save", "send"} {
		t.Run(stage, func(t *testing.T) {
			cause := errors.New("fixture failure")
			p := &resumeProbe{idle: true}
			switch stage {
			case "idle":
				p.idleErr = cause
			case "queue":
				p.queueErr = cause
			case "save":
				p.saveErr = cause
			case "send":
				p.sendErr = cause
			}
			original := &goal.State{Objective: "work", Status: goal.Finished, Outcome: goal.Stopped, Summary: "progress"}
			x := &extension{state: original}
			err := x.resume(p)
			if !errors.Is(err, cause) || !strings.Contains(err.Error(), "goal resume:") || x.resumePending || len(p.messages) != 0 {
				t.Fatalf("err=%v pending=%v messages=%v", err, x.resumePending, p.messages)
			}
			if stage != "send" && x.state != original {
				t.Fatal("failure replaced prior state")
			}
			if stage == "send" && (x.state.Outcome != goal.Stopped || !strings.Contains(x.state.Summary, "fixture failure")) {
				t.Fatalf("state=%+v", x.state)
			}
			p.idleErr, p.queueErr, p.saveErr, p.sendErr = nil, nil, nil, nil
			if err := x.resume(p); err != nil {
				t.Fatal(err)
			}
			if len(p.messages) != 1 {
				t.Fatal("retry did not continue")
			}
		})
	}
}
