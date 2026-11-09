package cursorbridge

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func readCall(id, path string) map[string]any {
	return map[string]any{"toolName": "read", "toolCallId": id, "args": map[string]string{"path": path}}
}

func readResult(id, path, text string) ToolResult {
	return ToolResult{Call: Proposal{ID: id, Name: "read", Args: []byte(`{"path":"` + path + `"}`)}, Text: text}
}

// callbackReply holds what a mock model received for one tool callback.
type callbackReply struct {
	mu     sync.Mutex
	status int
	body   string
}

func (c *callbackReply) set(status int, body string) {
	c.mu.Lock()
	c.status, c.body = status, body
	c.mu.Unlock()
}

func (c *callbackReply) get() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.body
}

func parkedTurn(t *testing.T, m *mockBridge, p *Process, emit func(Event) error) Outcome {
	t.Helper()
	out, err := p.Run(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", emit, TurnOptions{Park: true})
	if err != nil || out.Session == nil || len(out.Proposals) != 1 {
		t.Fatalf("expected a parked run: err=%v out=%+v", err, out)
	}
	return out
}

func TestParkedRunResumesWithTheRealToolResult(t *testing.T) {
	m := &mockBridge{t: t}
	var reply callbackReply
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		reply.set(m.post(callbackPath, call))
		finished(w, map[string]any{"inputTokens": 9})
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	out := parkedTurn(t, m, p, func(Event) error { return nil })
	if out.Proposals[0].ID != "c1" || out.Proposals[0].Name != "read" {
		t.Fatalf("proposal: %+v", out.Proposals[0])
	}
	if m.call("CloseAgent") != 0 || m.call("CancelRun") != 0 {
		t.Fatalf("a parked run must stay open: close=%d cancel=%d", m.call("CloseAgent"), m.call("CancelRun"))
	}
	if status, _ := reply.get(); status != 0 {
		t.Fatalf("callback was answered before PiG ran the tool: %d", status)
	}
	var text strings.Builder
	final, err := out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "file body")}, func(e Event) error { text.WriteString(e.Text); return nil }, TurnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if status, body := reply.get(); status != http.StatusOK || !strings.Contains(body, "file body") || !strings.Contains(body, `"isError":false`) {
		t.Fatalf("model got %d %s", status, body)
	}
	if final.Text != "final answer" || text.String() != "final answer" || final.Session != nil || final.CleanupError != nil {
		t.Fatalf("final: %+v streamed=%q", final, text.String())
	}
	if m.call("Send") != 1 || m.call("CreateAgent") != 1 || m.call("CloseAgent") != 1 || m.call("SetToolCallback") != 2 {
		t.Fatalf("lifecycle: send=%d create=%d close=%d callback=%d", m.call("Send"), m.call("CreateAgent"), m.call("CloseAgent"), m.call("SetToolCallback"))
	}
}

func TestResumeDeliversAnErrorResultAsAnError(t *testing.T) {
	m := &mockBridge{t: t}
	var reply callbackReply
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		reply.set(m.post(callbackPath, call))
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	out := parkedTurn(t, m, processFor(server), func(Event) error { return nil })
	result := readResult("c1", "a.go", "permission denied")
	result.IsError = true
	if _, err := out.Session.Resume(context.Background(), []ToolResult{result}, func(Event) error { return nil }, TurnOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, body := reply.get(); !strings.Contains(body, `"isError":true`) || !strings.Contains(body, "permission denied") {
		t.Fatalf("error result lost: %s", body)
	}
}

func TestResumeRefusesResultsThatDoNotMatchAndFabricatesNothing(t *testing.T) {
	for name, results := range map[string][]ToolResult{
		"different arguments": {readResult("c1", "other.go", "x")},
		"different id":        {readResult("zzz", "a.go", "x")},
		"no results":          nil,
		"extra result":        {readResult("c1", "a.go", "x"), readResult("c2", "b.go", "y")},
		"different tool":      {{Call: Proposal{ID: "c1", Name: "bash", Args: []byte(`{"path":"a.go"}`)}, Text: "x"}},
	} {
		t.Run(name, func(t *testing.T) {
			m := &mockBridge{t: t}
			var reply callbackReply
			m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
				call := readCall("c1", "a.go")
				call["agentId"] = agent
				reply.set(m.post(callbackPath, call))
				finished(w, nil)
			}
			server := m.server()
			defer server.Close()
			out := parkedTurn(t, m, processFor(server), func(Event) error { return nil })
			_, err := out.Session.Resume(context.Background(), results, func(Event) error { return nil }, TurnOptions{})
			if !errors.Is(err, ErrSessionGone) {
				t.Fatalf("err = %v", err)
			}
			if status, body := reply.get(); status == http.StatusOK || strings.Contains(body, `"result"`) {
				t.Fatalf("model received a result for a mismatched answer: %d %s", status, body)
			}
			if m.call("CloseAgent") != 1 {
				t.Fatalf("session not disposed: %d", m.call("CloseAgent"))
			}
			if _, err = out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{}); !errors.Is(err, ErrSessionGone) {
				t.Fatalf("second resume: %v", err)
			}
		})
	}
}

func TestParallelCallsAreAnsweredTogetherAndOnlyTogether(t *testing.T) {
	m := &mockBridge{t: t}
	var first, second callbackReply
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		var wg sync.WaitGroup
		for i, id := range []string{"a", "b"} {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				call := readCall(id, id+".go")
				call["agentId"] = agent
				status, body := m.post(callbackPath, call)
				[]*callbackReply{&first, &second}[i].set(status, body)
			}(i, id)
		}
		wg.Wait()
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	out, err := p.Run(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil }, TurnOptions{Park: true})
	if err != nil || out.Session == nil || len(out.Proposals) != 2 {
		t.Fatalf("%v %+v", err, out)
	}
	// One result for two pending calls is refused as a whole.
	if _, err = out.Session.Resume(context.Background(), []ToolResult{readResult("a", "a.go", "A")}, func(Event) error { return nil }, TurnOptions{}); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("partial answer: %v", err)
	}
	if status, _ := first.get(); status == http.StatusOK {
		t.Fatal("a partial answer reached the model")
	}
}

func TestParallelCallsResumeWithBothResults(t *testing.T) {
	m := &mockBridge{t: t}
	var first, second callbackReply
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		var wg sync.WaitGroup
		for i, id := range []string{"a", "b"} {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				call := readCall(id, id+".go")
				call["agentId"] = agent
				status, body := m.post(callbackPath, call)
				[]*callbackReply{&first, &second}[i].set(status, body)
			}(i, id)
		}
		wg.Wait()
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	out, err := p.Run(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil }, TurnOptions{Park: true})
	if err != nil || out.Session == nil || len(out.Proposals) != 2 {
		t.Fatalf("%v %+v", err, out)
	}
	results := []ToolResult{readResult("b", "b.go", "B"), readResult("a", "a.go", "A")} // any order
	if _, err = out.Session.Resume(context.Background(), results, func(Event) error { return nil }, TurnOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, body := first.get(); !strings.Contains(body, `"text":"A"`) {
		t.Fatalf("first call got %s", body)
	}
	if _, body := second.get(); !strings.Contains(body, `"text":"B"`) {
		t.Fatalf("second call got %s", body)
	}
}

func TestSecondRoundParksAgainOnTheSameAgent(t *testing.T) {
	m := &mockBridge{t: t}
	var second callbackReply
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		for i, id := range []string{"c1", "c2"} {
			call := readCall(id, "f"+id)
			call["agentId"] = agent
			status, body := m.post(callbackPath, call)
			if i == 1 {
				second.set(status, body)
			}
		}
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	first := parkedTurn(t, m, p, func(Event) error { return nil })
	mid, err := first.Session.Resume(context.Background(), []ToolResult{readResult("c1", "fc1", "one")}, func(Event) error { return nil }, TurnOptions{Park: true})
	if err != nil || mid.Session != first.Session || len(mid.Proposals) != 1 || mid.Proposals[0].ID != "c2" {
		t.Fatalf("second round: %v %+v", err, mid)
	}
	if m.call("CloseAgent") != 0 || m.call("CreateAgent") != 1 {
		t.Fatalf("agent changed between rounds: close=%d create=%d", m.call("CloseAgent"), m.call("CreateAgent"))
	}
	final, err := mid.Session.Resume(context.Background(), []ToolResult{readResult("c2", "fc2", "two")}, func(Event) error { return nil }, TurnOptions{})
	if err != nil || final.Text != "final answer" {
		t.Fatalf("%v %+v", err, final)
	}
	if _, body := second.get(); !strings.Contains(body, "two") {
		t.Fatalf("second result lost: %s", body)
	}
	if m.call("Send") != 1 || m.call("CloseAgent") != 1 {
		t.Fatalf("send=%d close=%d", m.call("Send"), m.call("CloseAgent"))
	}
}

func TestAnsweredIdentifierCannotBeReused(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		m.post(callbackPath, call) // replay of an answered call
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	out := parkedTurn(t, m, processFor(server), func(Event) error { return nil })
	_, err := out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{})
	if err == nil || !strings.Contains(err.Error(), "answered tool call") {
		t.Fatalf("reused identifier accepted: %v", err)
	}
}

func TestNewTurnClosesTheParkedRun(t *testing.T) {
	m := &mockBridge{t: t}
	var n int
	var mu sync.Mutex
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		mu.Lock()
		n++
		mine := n
		mu.Unlock()
		if mine == 1 {
			call := readCall("c1", "a.go")
			call["agentId"] = agent
			m.post(callbackPath, call)
		}
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	out := parkedTurn(t, m, p, func(Event) error { return nil })
	other, err := p.RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "another conversation", func(Event) error { return nil })
	if err != nil || other.Text != "final answer" {
		t.Fatalf("%v %+v", err, other)
	}
	if m.call("CloseAgent") != 2 {
		t.Fatalf("parked agent not closed: %d", m.call("CloseAgent"))
	}
	if _, err = out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{}); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("resume of a replaced run: %v", err)
	}
}

func TestParkedRunDoesNotBlockAnotherTurn(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		if !strings.Contains(m.sendBody, "second") {
			call := readCall("c1", "a.go")
			call["agentId"] = agent
			m.post(callbackPath, call)
		}
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	parkedTurn(t, m, p, func(Event) error { return nil })
	done := make(chan error, 1)
	go func() {
		_, err := p.RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "second", func(Event) error { return nil })
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a parked run blocked the next turn")
	}
}

func TestParkedRunTimesOut(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	out, err := p.Run(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil }, TurnOptions{Park: true, ParkTimeout: 80 * time.Millisecond})
	if err != nil || out.Session == nil {
		t.Fatalf("%v %+v", err, out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for m.call("CloseAgent") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.call("CloseAgent") != 1 {
		t.Fatal("timed out run was not closed")
	}
	if _, err = out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{}); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("resume after timeout: %v", err)
	}
}

func TestCloseIsIdempotentAndReleaseParkedEndsTheRun(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	out := parkedTurn(t, m, p, func(Event) error { return nil })
	if err := out.Session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Session.Close(); err != nil {
		t.Fatal(err)
	}
	if m.call("CloseAgent") != 1 || m.call("SetToolCallback") != 2 {
		t.Fatalf("close=%d callback=%d", m.call("CloseAgent"), m.call("SetToolCallback"))
	}
	again := parkedTurn(t, m, p, func(Event) error { return nil })
	p.releaseParked()
	if p.closeErr != nil {
		t.Fatal(p.closeErr)
	}
	if _, err := again.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{}); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("resume after Process.Close: %v", err)
	}
}

func TestCancelledResumeDisposesTheRun(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		<-r.Context().Done() // the model keeps working until cancelled
	}
	server := m.server()
	defer server.Close()
	out := parkedTurn(t, m, processFor(server), func(Event) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	if _, err := out.Session.Resume(ctx, []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if m.call("CloseAgent") != 1 {
		t.Fatalf("cancelled resume left the agent open: %d", m.call("CloseAgent"))
	}
}

func TestOutputWhileParkedIsDeliveredOnResume(t *testing.T) {
	m := &mockBridge{t: t}
	release := make(chan struct{})
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		go func() { m.post(callbackPath, call) }()
		<-release
		deltaFrames(w, map[string]any{"type": "text-delta", "update": map[string]any{"text": "late "}})
		w.(http.Flusher).Flush()
		time.Sleep(50 * time.Millisecond)
		deltaFrames(w, map[string]any{"type": "text-delta", "update": map[string]any{"text": "words"}})
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	var early strings.Builder
	out := parkedTurn(t, m, processFor(server), func(e Event) error { early.WriteString(e.Text); return nil })
	close(release)
	time.Sleep(20 * time.Millisecond) // "late " arrives while parked
	var later strings.Builder
	final, err := out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(e Event) error { later.WriteString(e.Text); return nil }, TurnOptions{})
	if err != nil || early.Len() != 0 || later.String() != "late words" || final.Text != "final answer" {
		t.Fatalf("err=%v early=%q later=%q final=%q", err, early.String(), later.String(), final.Text)
	}
}

func TestTurnWithoutParkStillTearsDownAtOnce(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		<-r.Context().Done()
	}
	server := m.server()
	defer server.Close()
	out, err := processFor(server).RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil })
	if err != nil || len(out.Proposals) != 1 || out.Session != nil || m.call("CloseAgent") != 1 {
		t.Fatalf("%v %+v close=%d", err, out, m.call("CloseAgent"))
	}
}
