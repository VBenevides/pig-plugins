package guard

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUnknownScopeAdvice(t *testing.T) {
	for _, tt := range []struct {
		name    string
		command string
		mode    Mode
		hasUI   bool
		fail    bool
		wantAsk bool
		wantLLM bool
	}{
		{"unknown", "kill -9 $PID", Interactive, true, false, true, true},
		{"failed model", "kill -9 $PID", Interactive, true, true, true, true},
		{"known", "git push --force origin dev", Interactive, true, false, true, false},
		{"safe", "pwd", Interactive, true, false, false, false},
		{"hard block", "rm -rf /", Interactive, true, false, false, false},
		{"strict", "kill -9 $PID", Strict, true, false, false, false},
		{"no UI", "kill -9 $PID", Interactive, false, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gate := newGate(tt.mode, false, nil)
			calls, asks := 0, 0
			decision := gate.Check(context.Background(), Call{
				Tool: "bash", Input: bash(tt.command), Cwd: "/work", HasUI: tt.hasUI,
				ExplainUnknown: func(command, cwd string) (ScopeExplanation, error) {
					calls++
					if command != tt.command || cwd != "/work" {
						t.Fatalf("unexpected assessment input %q, %q", command, cwd)
					}
					if tt.fail {
						return ScopeExplanation{}, errors.New("provider failure")
					}
					return ScopeExplanation{Risk: "Kills processes", AffectedItems: []string{"process: $PID - SIGKILL; PID unknown"}}, nil
				},
				Confirm: func(title, body string) (bool, error) {
					asks++
					if tt.wantLLM {
						want := "Model assessment (advisory; not verified):"
						if tt.fail {
							want = "Model assessment unavailable"
						} else if !strings.Contains(body, "Risk: Kills processes") || !strings.Contains(body, "process: $PID") {
							t.Errorf("missing model explanation: %s", body)
						}
						if !strings.Contains(body, want) || !strings.Contains(body, "scope: unknown") {
							t.Errorf("missing advice or uncertainty: %s", body)
						}
					}
					return false, nil
				},
			})
			if (calls > 0) != tt.wantLLM || calls > 1 || (asks > 0) != tt.wantAsk {
				t.Errorf("model calls=%d asks=%d", calls, asks)
			}
			if tt.wantAsk && !decision.Block {
				t.Error("model advice bypassed user denial")
			}
		})
	}
}

func TestSavedGrantSkipsScopeModel(t *testing.T) {
	cwd := workDir(t)
	gate := newGate(Interactive, false, nil)
	gate.UseAllowlist(NewAllowlist(t.TempDir() + "/allow.json"))
	calls := 0
	call := selectCall(cwd, "bash", bash("kill -9 $PID"), &selector{choice: choiceAlways})
	call.ExplainUnknown = func(_, _ string) (ScopeExplanation, error) {
		calls++
		return ScopeExplanation{Risk: "Kills a process", AffectedItems: []string{"process: unknown"}}, nil
	}
	if got := gate.Check(context.Background(), call); got.Block {
		t.Fatalf("first approval blocked: %+v", got)
	}
	call.Select = (&selector{choice: choiceDeny}).selectOne
	if got := gate.Check(context.Background(), call); got.Block || calls != 1 {
		t.Fatalf("saved grant: decision=%+v model calls=%d", got, calls)
	}
}

func TestScopeAdviceIsBoundedAndSanitized(t *testing.T) {
	call := Call{ExplainUnknown: func(_, _ string) (ScopeExplanation, error) {
		return ScopeExplanation{Risk: "risk\n\x1b[31m", AffectedItems: []string{strings.Repeat("x", 600)}}, nil
	}}
	text := unknownScopeAdvice(call, "command")
	if strings.Contains(text, "\x1b") || strings.Contains(text, strings.Repeat("x", 501)) || !strings.Contains(text, "Risk: risk ") {
		t.Fatalf("unsafe or unbounded advice: %q", text)
	}
	call.ExplainUnknown = func(_, _ string) (ScopeExplanation, error) { return ScopeExplanation{}, nil }
	if text := unknownScopeAdvice(call, "command"); !strings.Contains(text, "incomplete response") {
		t.Fatalf("missing failure notice: %q", text)
	}
}
