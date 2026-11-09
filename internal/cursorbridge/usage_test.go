package cursorbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func usageMessage(w http.ResponseWriter, in, out, cacheRead int) {
	frame(w, 0, map[string]any{"sdkMessage": map[string]any{"type": "usage", "usage": map[string]any{
		"inputTokens": in, "outputTokens": out, "cacheReadTokens": cacheRead, "cacheWriteTokens": 0, "totalTokens": in + out}}})
}

func TestUsageArithmetic(t *testing.T) {
	a := TokenUsage{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 60, TotalTokens: 110}
	b := TokenUsage{InputTokens: 30, OutputTokens: 4, CacheReadTokens: 20, TotalTokens: 34}
	if sum := a.Add(b); sum.InputTokens != 130 || sum.CacheReadTokens != 80 || sum.TotalTokens != 144 {
		t.Fatalf("add %+v", sum)
	}
	if diff := a.Sub(b); diff.InputTokens != 70 || diff.CacheReadTokens != 40 {
		t.Fatalf("sub %+v", diff)
	}
	if diff := b.Sub(a); !diff.IsZero() {
		t.Fatalf("a lower cumulative must add nothing, got %+v", diff)
	}
	if a.UncachedInput() != 40 || (TokenUsage{InputTokens: 100, CacheReadTokens: 30, CacheWriteTokens: 20}).UncachedInput() != 50 || (TokenUsage{InputTokens: 5, CacheReadTokens: 9}).UncachedInput() != 0 {
		t.Fatal("uncached input")
	}
}

func TestUsageFrameReadsOnlyUsageMessages(t *testing.T) {
	good := json.RawMessage(`{"sdkMessage":{"type":"usage","usage":{"inputTokens":"12","outputTokens":3,"cacheReadTokens":"5"}}}`)
	if u := usageFrame(good); u == nil || u.InputTokens != 12 || u.OutputTokens != 3 || u.CacheReadTokens != 5 {
		t.Fatalf("usage %+v", u)
	}
	for _, raw := range []string{`{"sdkMessage":{"type":"assistant"}}`, `{"interactionUpdate":{"type":"text-delta"}}`, `not json`, `{"sdkMessage":{"type":"usage"}}`} {
		if usageFrame(json.RawMessage(raw)) != nil {
			t.Fatalf("frame %s read as usage", raw)
		}
	}
}

func TestEachRoundReportsOnlyItsShareOfTheRunUsage(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		usageMessage(w, 1000, 20, 100) // model call 1, ends in the tool call
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		usageMessage(w, 1500, 30, 1000) // model call 2
		// The run total is cumulative: both calls.
		finished(w, map[string]any{"inputTokens": 2500, "outputTokens": 50, "cacheReadTokens": 1100, "totalTokens": 2550})
	}
	server := m.server()
	defer server.Close()
	out := parkedTurn(t, m, processFor(server), func(Event) error { return nil })
	if out.Usage == nil || out.Usage.InputTokens != 1000 || out.Usage.CacheReadTokens != 100 || out.Usage.OutputTokens != 20 {
		t.Fatalf("round 1 usage %+v", out.Usage)
	}
	if len(out.ModelCalls) != 1 || out.ModelCalls[0].InputTokens != 1000 {
		t.Fatalf("round 1 calls %+v", out.ModelCalls)
	}
	final, err := out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if final.Usage == nil || final.Usage.InputTokens != 1500 || final.Usage.CacheReadTokens != 1000 || final.Usage.OutputTokens != 30 {
		t.Fatalf("round 2 must report only its own share: %+v", final.Usage)
	}
	if round1, round2 := out.Usage.Add(*final.Usage), (TokenUsage{InputTokens: 2500, OutputTokens: 50, CacheReadTokens: 1100, TotalTokens: 2550}); round1.InputTokens != round2.InputTokens || round1.OutputTokens != round2.OutputTokens || round1.CacheReadTokens != round2.CacheReadTokens {
		t.Fatalf("rounds do not add up to the run total: %+v", round1)
	}
}

func TestUsageThatOnlyArrivesAtTheEndIsReportedInTheFinalRound(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		call := readCall("c1", "a.go")
		call["agentId"] = agent
		m.post(callbackPath, call)
		finished(w, map[string]any{"inputTokens": 2500, "outputTokens": 50, "cacheReadTokens": 1100})
	}
	server := m.server()
	defer server.Close()
	out := parkedTurn(t, m, processFor(server), func(Event) error { return nil })
	if out.Usage != nil {
		t.Fatalf("round 1 invented usage: %+v", out.Usage)
	}
	final, err := out.Session.Resume(context.Background(), []ToolResult{readResult("c1", "a.go", "x")}, func(Event) error { return nil }, TurnOptions{})
	if err != nil || final.Usage == nil || final.Usage.InputTokens != 2500 || final.Usage.CacheReadTokens != 1100 {
		t.Fatalf("%v %+v", err, final.Usage)
	}
}

func TestRunTotalIsUsedWhenNoPerCallUsageStreamed(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		finished(w, map[string]any{"inputTokens": 900, "outputTokens": 11, "cacheReadTokens": 300})
	}
	server := m.server()
	defer server.Close()
	out, err := processFor(server).RunTurn(context.Background(), "k", Selection{ID: "grok"}, readTool, "p", func(Event) error { return nil })
	if err != nil || out.Usage == nil || out.Usage.InputTokens != 900 || out.Usage.CacheReadTokens != 300 || len(out.ModelCalls) != 0 {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestNoUsageStaysNil(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) { finished(w, nil) }
	server := m.server()
	defer server.Close()
	out, err := processFor(server).RunTurn(context.Background(), "k", Selection{ID: "grok"}, readTool, "p", func(Event) error { return nil })
	if err != nil || out.Usage != nil {
		t.Fatalf("%v %+v", err, out.Usage)
	}
}
