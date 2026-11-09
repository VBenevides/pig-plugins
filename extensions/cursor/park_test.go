package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

// fakeParked stands in for a Cursor run that waits on tool results.
type fakeParked struct {
	mu        sync.Mutex
	proposals []cursorbridge.Proposal
	outcome   cursorbridge.Outcome
	err       error
	events    []cursorbridge.Event
	resumed   [][]cursorbridge.ToolResult
	closed    int
	done      chan struct{}
	once      sync.Once
}

func newFakeParked(proposals ...cursorbridge.Proposal) *fakeParked {
	return &fakeParked{proposals: proposals, done: make(chan struct{})}
}

func (f *fakeParked) Proposals() []cursorbridge.Proposal { return f.proposals }
func (f *fakeParked) Done() <-chan struct{}              { return f.done }
func (f *fakeParked) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	f.once.Do(func() { close(f.done) })
	return nil
}
func (f *fakeParked) Resume(_ context.Context, results []cursorbridge.ToolResult, emit func(cursorbridge.Event) error, _ cursorbridge.TurnOptions) (cursorbridge.Outcome, error) {
	f.mu.Lock()
	f.resumed = append(f.resumed, results)
	f.mu.Unlock()
	for _, event := range f.events {
		if err := emit(event); err != nil {
			return cursorbridge.Outcome{}, err
		}
	}
	if f.outcome.Session == nil {
		f.once.Do(func() { close(f.done) }) // the run ended, as a real session does
	}
	return f.outcome, f.err
}
func (f *fakeParked) resumeCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.resumed) }

var readCallProposal = cursorbridge.Proposal{ID: "c1", Name: "read", Args: json.RawMessage(`{"path":"a.go"}`)}

func readToolDecl() map[string]any {
	return map[string]any{"name": "read", "description": "read", "parameters": map[string]any{"type": "object"}}
}

// followUp is the request PiG sends after it ran the parked call: the earlier
// messages, the assistant message with the call, then the real result.
func followUp(base map[string]any, results ...map[string]any) map[string]any {
	messages := append([]any{}, base["messages"].([]any)...)
	messages = append(messages, map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "text", "text": "reading"},
		map[string]any{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{"path": "a.go"}},
	}})
	for _, result := range results {
		messages = append(messages, result)
	}
	next := map[string]any{"systemPrompt": base["systemPrompt"], "tools": base["tools"], "messages": messages}
	return next
}

func toolResultMessage(id, name, text string) map[string]any {
	return map[string]any{"role": "toolResult", "toolCallId": id, "toolName": name, "content": []any{map[string]any{"type": "text", "text": text}}}
}

// parkedService runs a first turn that parks and returns what later turns need.
func parkedService(t *testing.T) (*service, *fakeRunner, *fakeParked, map[string]any) {
	t.Helper()
	return parkedServiceFor(t, []cursorbridge.Model{{ID: "plain"}}, "plain", "")
}

func parkedServiceFor(t *testing.T, catalog []cursorbridge.Model, modelID, level string) (*service, *fakeRunner, *fakeParked, map[string]any) {
	t.Helper()
	parked := newFakeParked(readCallProposal)
	r := &fakeRunner{catalog: catalog, outcome: cursorbridge.Outcome{Proposals: []cursorbridge.Proposal{readCallProposal}}, parkNext: parked}
	s, _ := newService(map[string]*fakeRunner{"k": r})
	first := request("go", readToolDecl())
	stream, err := s.stream(map[string]any{"id": modelID}, first, opts("k", level))
	if err != nil {
		t.Fatal(err)
	}
	if _, final := collect(t, stream); final["stopReason"] != "toolUse" {
		t.Fatalf("first turn: %v", final)
	}
	if len(r.parkOpts) != 1 || !r.parkOpts[0].Park {
		t.Fatalf("first turn did not ask to park: %+v", r.parkOpts)
	}
	return s, r, parked, first
}

func runFollowUp(t *testing.T, s *service, req map[string]any, level string) map[string]any {
	t.Helper()
	return runFollowUpOn(t, s, "plain", req, level)
}

func runFollowUpOn(t *testing.T, s *service, modelID string, req map[string]any, level string) map[string]any {
	t.Helper()
	stream, err := s.stream(map[string]any{"id": modelID}, req, opts("k", level))
	if err != nil {
		t.Fatal(err)
	}
	_, final := collect(t, stream)
	return final
}

func TestToolResultResumesTheParkedRunWithoutReplay(t *testing.T) {
	s, r, parked, first := parkedService(t)
	parked.events = []cursorbridge.Event{{Text: "the third line is charlie"}}
	parked.outcome = cursorbridge.Outcome{Text: "the third line is charlie", Usage: &cursorbridge.TokenUsage{TotalTokens: 5}}
	req := followUp(first, toolResultMessage("c1", "read", "alpha\nbravo\ncharlie"))
	final := runFollowUp(t, s, req, "")
	if final["stopReason"] != "stop" || final["content"].([]any)[0].(map[string]any)["text"] != "the third line is charlie" {
		t.Fatalf("final %v", final)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("the transcript was replayed: %d turns started", len(r.prompts))
	}
	if parked.resumeCount() != 1 {
		t.Fatalf("resume count %d", parked.resumeCount())
	}
	got := parked.resumed[0]
	if len(got) != 1 || got[0].Call.ID != "c1" || got[0].Text != "alpha\nbravo\ncharlie" || got[0].IsError {
		t.Fatalf("results %+v", got)
	}
}

func TestFailedToolResultIsResumedAsAnError(t *testing.T) {
	s, _, parked, first := parkedService(t)
	failed := toolResultMessage("c1", "read", "no such file")
	failed["isError"] = true
	runFollowUp(t, s, followUp(first, failed), "")
	if parked.resumeCount() != 1 || !parked.resumed[0][0].IsError {
		t.Fatalf("error flag lost: %+v", parked.resumed)
	}
}

func TestOnlyTheExactParkedConversationResumes(t *testing.T) {
	edited := func(first map[string]any) map[string]any {
		req := followUp(first, toolResultMessage("c1", "read", "x"))
		req["messages"].([]any)[0] = map[string]any{"role": "user", "content": "a different question"}
		return req
	}
	cases := map[string]func(first map[string]any) map[string]any{
		"edited earlier message": edited,
		"another system prompt": func(first map[string]any) map[string]any {
			req := followUp(first, toolResultMessage("c1", "read", "x"))
			req["systemPrompt"] = "other"
			return req
		},
		"no result yet": func(first map[string]any) map[string]any { return followUp(first) },
		"result of another call": func(first map[string]any) map[string]any {
			return followUp(first, toolResultMessage("zzz", "read", "x"))
		},
		"result names another tool": func(first map[string]any) map[string]any {
			return followUp(first, toolResultMessage("c1", "bash", "x"))
		},
		"user message after the result": func(first map[string]any) map[string]any {
			return followUp(first, toolResultMessage("c1", "read", "x"), map[string]any{"role": "user", "content": "also this"})
		},
		"duplicate result": func(first map[string]any) map[string]any {
			return followUp(first, toolResultMessage("c1", "read", "x"), toolResultMessage("c1", "read", "x"))
		},
		"host changed the call arguments": func(first map[string]any) map[string]any {
			req := followUp(first, toolResultMessage("c1", "read", "x"))
			msgs := req["messages"].([]any)
			msgs[1].(map[string]any)["content"].([]any)[1].(map[string]any)["arguments"] = map[string]any{"path": "other.go"}
			return req
		},
		"a new user turn": func(first map[string]any) map[string]any {
			req := followUp(first, toolResultMessage("c1", "read", "x"))
			req["messages"] = append(req["messages"].([]any), map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "done"}}}, map[string]any{"role": "user", "content": "next"})
			return req
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			s, r, parked, first := parkedService(t)
			runFollowUp(t, s, build(first), "")
			if parked.resumeCount() != 0 {
				t.Fatal("a request that is not the parked conversation resumed the run")
			}
			if len(r.prompts) != 2 {
				t.Fatalf("expected a replay on a fresh agent, got %d turns", len(r.prompts))
			}
		})
	}
}

func TestChangedModelOrEffortReplaysInsteadOfResuming(t *testing.T) {
	for name, follow := range map[string]struct{ model, level string }{
		"another effort level": {"grok-4.6-fast", "high"},
		"another model":        {"grok-4.7-fast", "low"},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := []cursorbridge.Model{{ID: "grok-4.6-low-fast"}, {ID: "grok-4.6-high-fast"}, {ID: "grok-4.7-low-fast"}, {ID: "grok-4.7-high-fast"}}
			s, r, parked, first := parkedServiceFor(t, catalog, "grok-4.6-fast", "low")
			r.outcome = cursorbridge.Outcome{}
			final := runFollowUpOn(t, s, follow.model, followUp(first, toolResultMessage("c1", "read", "x")), follow.level)
			if final["stopReason"] != "stop" || len(r.prompts) != 2 {
				t.Fatalf("final %v turns %d: the turn should replay on a fresh agent", final, len(r.prompts))
			}
			if parked.resumeCount() != 0 {
				t.Fatal("resumed a run that was started with another model or effort")
			}
		})
	}
}

func TestLostSessionFallsBackToReplay(t *testing.T) {
	s, r, parked, first := parkedService(t)
	parked.err = errors.Join(cursorbridge.ErrSessionGone, errors.New("timed out"))
	r.outcome = cursorbridge.Outcome{}
	r.events = []cursorbridge.Event{{Text: "replayed answer"}}
	final := runFollowUp(t, s, followUp(first, toolResultMessage("c1", "read", "x")), "")
	if final["stopReason"] != "stop" || len(r.prompts) != 2 {
		t.Fatalf("final %v turns %d", final, len(r.prompts))
	}
	if final["content"].([]any)[0].(map[string]any)["text"] != "replayed answer" {
		t.Fatalf("content %v", final["content"])
	}
}

func TestFailureAfterResumeIsNeverReplayed(t *testing.T) {
	s, r, parked, first := parkedService(t)
	parked.err = errors.New("Cursor run failed")
	final := runFollowUp(t, s, followUp(first, toolResultMessage("c1", "read", "x")), "")
	if final["stopReason"] != "error" || len(r.prompts) != 1 {
		t.Fatalf("final %v turns %d: a run that already continued remotely must not be replayed", final, len(r.prompts))
	}
}

func TestResumedRunThatParksAgainKeepsTheChain(t *testing.T) {
	s, r, parked, first := parkedService(t)
	second := cursorbridge.Proposal{ID: "c2", Name: "read", Args: json.RawMessage(`{"path":"b.go"}`)}

	// Round 2: the run asks for another call, so it parks again (non-nil Session).
	parked.outcome = cursorbridge.Outcome{Proposals: []cursorbridge.Proposal{second}, Session: &cursorbridge.Session{}}
	req2 := followUp(first, toolResultMessage("c1", "read", "A"))
	if final := runFollowUp(t, s, req2, ""); final["stopReason"] != "toolUse" {
		t.Fatalf("second turn %v", final)
	}

	// Round 3: the session now waits on c2. Its answer resumes the same run.
	parked.mu.Lock()
	parked.proposals = []cursorbridge.Proposal{second}
	parked.outcome = cursorbridge.Outcome{Text: "done"}
	parked.events = []cursorbridge.Event{{Text: "done"}}
	parked.mu.Unlock()
	messages := append([]any{}, req2["messages"].([]any)...)
	messages = append(messages,
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "c2", "name": "read", "arguments": map[string]any{"path": "b.go"}}}},
		toolResultMessage("c2", "read", "B"))
	req3 := map[string]any{"systemPrompt": first["systemPrompt"], "tools": first["tools"], "messages": messages}
	if final := runFollowUp(t, s, req3, ""); final["stopReason"] != "stop" {
		t.Fatalf("third turn %v", final)
	}
	if parked.resumeCount() != 2 || len(r.prompts) != 1 {
		t.Fatalf("resumes %d, fresh turns %d: the chain should use one agent", parked.resumeCount(), len(r.prompts))
	}
	if got := parked.resumed[1]; len(got) != 1 || got[0].Call.ID != "c2" || got[0].Text != "B" {
		t.Fatalf("second resume %+v", got)
	}
}

func TestParkingCanBeTurnedOff(t *testing.T) {
	t.Setenv("PIG_CURSOR_PARK", "0")
	r := &fakeRunner{catalog: []cursorbridge.Model{{ID: "plain"}}, outcome: cursorbridge.Outcome{Proposals: []cursorbridge.Proposal{readCallProposal}}, parkNext: newFakeParked(readCallProposal)}
	s, _ := newService(map[string]*fakeRunner{"k": r})
	runFollowUp(t, s, request("go", readToolDecl()), "")
	if len(r.parkOpts) != 1 || r.parkOpts[0].Park {
		t.Fatalf("parking was not disabled: %+v", r.parkOpts)
	}
}

func TestTurnsWithoutToolsNeverPark(t *testing.T) {
	r := &fakeRunner{catalog: []cursorbridge.Model{{ID: "plain"}}, events: []cursorbridge.Event{{Text: "hi"}}}
	s, _ := newService(map[string]*fakeRunner{"k": r})
	runFollowUp(t, s, request("go"), "")
	if r.parkOpts[0].Park {
		t.Fatal("a turn with no tools asked to park")
	}
}

func TestParkedRunHoldsTheBridgeUntilItEnds(t *testing.T) {
	s, _, parked, _ := parkedService(t)
	id := accountHash("k")
	users := func() int {
		s.manager.mu.Lock()
		defer s.manager.mu.Unlock()
		return s.manager.entries[id].users
	}
	if users() != 1 {
		t.Fatalf("a parked run must keep the bridge process in use, users = %d", users())
	}
	_ = parked.Close()
	deadline := time.Now().Add(2 * time.Second)
	for users() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if users() != 0 {
		t.Fatalf("the bridge process stayed pinned after the run ended: %d", users())
	}
	if s.parked.peek(id) != nil {
		t.Fatal("ended run is still registered")
	}
}
