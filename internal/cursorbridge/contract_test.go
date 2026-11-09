package cursorbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCustomToolsRequireOnlyInternalMCPChannel(t *testing.T) {
	var options map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Options map[string]json.RawMessage `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		options = request.Options
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"agentId":"agent"}`)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err = client.Create(context.Background(), "key", Selection{ID: "grok-4.6"}, readTool); err != nil {
		t.Fatal(err)
	}
	if string(options["tools"]) != `{"names":["mcp"]}` {
		t.Fatalf("custom tools unreachable: tools=%s", options["tools"])
	}
	if string(options["mcpServers"]) != `{}` || string(options["agents"]) != `{}` {
		t.Fatal("external execution channels enabled")
	}
	var local struct {
		SettingSources     []string       `json:"settingSources"`
		AutoReview         bool           `json:"autoReview"`
		EnableAgentRetries *bool          `json:"enableAgentRetries"`
		CustomTools        map[string]any `json:"customTools"`
	}
	if err = json.Unmarshal(options["local"], &local); err != nil {
		t.Fatal(err)
	}
	if len(local.SettingSources) != 0 || local.AutoReview || local.EnableAgentRetries == nil || *local.EnableAgentRetries || local.CustomTools["read"] == nil {
		t.Fatal("unsafe custom tool configuration")
	}
}

func TestCallbackParksWithoutFabricatingAToolResult(t *testing.T) {
	proposed := make(chan struct{}, 1)
	server, err := NewToolServer(readTool, func() { proposed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.Expect("agent")
	done := make(chan int, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+callbackPath, strings.NewReader(`{"agentId":"agent","toolName":"read","toolCallId":"call-1","args":{"path":"README.md"}}`))
		if err != nil {
			done <- 0
			return
		}
		req.Header.Set("Authorization", "Bearer "+server.Token)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		defer response.Body.Close()
		done <- response.StatusCode
	}()
	select {
	case <-proposed:
	case <-ctx.Done():
		t.Fatal("proposal not published")
	}
	select {
	case status := <-done:
		t.Fatalf("callback returned a fabricated result before PiG execution: HTTP %d", status)
	case <-time.After(30 * time.Millisecond):
	}
	proposals, err := server.Results()
	if err != nil || len(proposals) != 1 {
		t.Fatal(proposals, err)
	}
	if err = server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-done:
		if status == http.StatusOK {
			t.Fatal("closed callback returned a fabricated success")
		}
	case <-ctx.Done():
		t.Fatal("parked callback did not close")
	}
}

func TestOnlyOwnedToolNamespacesAreAccepted(t *testing.T) {
	allowed := map[string]bool{"read": true}
	for _, name := range []string{"foreign__read", "foreign:read", "foreign.read", "read", "Shell", "Glob"} {
		raw, _ := json.Marshal(map[string]any{"sdkMessage": map[string]any{"type": "tool_call", "message": map[string]any{"name": name}}})
		if _, err := classify(raw, allowed); err == nil {
			t.Errorf("accepted unowned tool event %q", name)
		}
	}
	for _, payload := range []map[string]any{
		{"name": "mcp", "args": map[string]any{"providerIdentifier": "custom-user-tools", "toolName": "read"}},
		{"name": "mcp__custom-user-tools__read"},
		{"name": "GetMcpTools"},
	} {
		raw, _ := json.Marshal(map[string]any{"sdkMessage": map[string]any{"type": "tool_call", "message": payload}})
		if _, err := classify(raw, allowed); err != nil {
			t.Errorf("refused SDK custom-tool channel: %v", err)
		}
	}
	raw := json.RawMessage(`{"sdkMessage":{"type":"tool_call","message":{"name":"mcp","args":{"providerIdentifier":"foreign","toolName":"read"}}}}`)
	if _, err := classify(raw, allowed); err == nil {
		t.Fatal("foreign MCP provider accepted")
	}
}

func TestThinkingOnlyStreamStillReturnsFinalAnswer(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(_ *mockBridge, w http.ResponseWriter, _ *http.Request, _ string) {
		frame(w, 0, map[string]any{"sdkMessage": map[string]any{"type": "thinking", "message": map[string]any{"text": "analysis"}}})
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	var seen []Event
	_, err := processFor(server).RunTurn(context.Background(), "key", Selection{ID: "grok"}, nil, "prompt", func(event Event) error { seen = append(seen, event); return nil })
	if err != nil || len(seen) != 2 || !seen[0].Thinking || seen[1].Thinking || seen[1].Text != "final answer" {
		t.Fatalf("%v %+v", err, seen)
	}
}

func deltaFrames(w http.ResponseWriter, updates ...map[string]any) {
	for _, update := range updates {
		frame(w, 0, map[string]any{"interactionUpdate": update})
	}
}

func TestStreamedDeltasPrintOnceAndSendRequestsThem(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(_ *mockBridge, w http.ResponseWriter, _ *http.Request, _ string) {
		deltaFrames(w,
			map[string]any{"type": "thinking-delta", "update": map[string]any{"text": "plan "}},
			map[string]any{"type": "thinking-delta", "update": map[string]any{"text": "it"}},
			map[string]any{"type": "text-delta", "update": map[string]any{"text": "final "}},
			map[string]any{"type": "text-delta", "update": map[string]any{"text": "answer"}},
		)
		// Completed blocks repeat the text that already streamed.
		frame(w, 0, map[string]any{"sdkMessage": map[string]any{"type": "thinking", "message": map[string]any{"text": "plan it"}}})
		frame(w, 0, map[string]any{"sdkMessage": map[string]any{"type": "assistant", "message": map[string]any{"message": map[string]any{"content": []any{map[string]string{"type": "text", "text": "final answer"}}}}}})
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	var thinking, text strings.Builder
	out, err := processFor(server).RunTurn(context.Background(), "key", Selection{ID: "grok"}, nil, "prompt", func(event Event) error {
		if event.Thinking {
			thinking.WriteString(event.Text)
		} else {
			text.WriteString(event.Text)
		}
		return nil
	})
	if err != nil || thinking.String() != "plan it" || text.String() != "final answer" || out.Text != "final answer" {
		t.Fatalf("%v thinking=%q text=%q out=%q", err, thinking.String(), text.String(), out.Text)
	}
	m.mu.Lock()
	sent := m.sendBody
	m.mu.Unlock()
	if !strings.Contains(sent, `"enableDeltas":true`) || !strings.Contains(sent, `"enableSteps":false`) {
		t.Fatalf("send options missing: %s", sent)
	}
}

func TestUnmatchedBlockIsKeptRatherThanLost(t *testing.T) {
	var d deltaTracker
	if !d.keep(Event{Text: "streamed", delta: true}) {
		t.Fatal("delta dropped")
	}
	if !d.keep(Event{Text: "different text"}) {
		t.Fatal("block with unseen text was dropped")
	}
	if !d.keep(Event{Text: "block without deltas"}) {
		t.Fatal("block was dropped when no delta preceded it")
	}
}

func TestPartialToolCallsStayInsideTheOwnedChannel(t *testing.T) {
	allowed := map[string]bool{"read": true}
	partial := func(call string) string {
		return `{"interactionUpdate":{"type":"partial-tool-call","update":{"toolCall":` + call + `}}}`
	}
	for name, c := range map[string]struct {
		raw string
		ok  bool
	}{
		"mcp args not streamed yet": {partial(`{"type":"mcp","args":{}}`), true},
		"owned declared tool":       {partial(`{"type":"mcp","args":{"providerIdentifier":"custom-user-tools","toolName":"read"}}`), true},
		"undeclared tool":           {partial(`{"type":"mcp","args":{"providerIdentifier":"custom-user-tools","toolName":"bash"}}`), false},
		"foreign provider":          {partial(`{"type":"mcp","args":{"providerIdentifier":"other","toolName":"read"}}`), false},
		"native shell":              {partial(`{"type":"shell","args":{}}`), false},
		"no tool type":              {partial(`{}`), false},
	} {
		_, err := classify(json.RawMessage(c.raw), allowed)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", name, err, c.ok)
		}
	}
	if _, err := classify(json.RawMessage(partial(`{"type":"mcp","args":{}}`)), nil); err == nil {
		t.Error("partial call accepted when PiG offered no tools")
	}
}

func TestRunawayOutputCancelsWithoutRetry(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(_ *mockBridge, w http.ResponseWriter, _ *http.Request, _ string) {
		frame(w, 0, map[string]any{"sdkMessage": map[string]any{"agentId": "agent-1", "runId": "r", "type": "thinking", "message": map[string]any{"text": strings.Repeat("x", maxTurnOutputBytes+1)}}})
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	_, err := processFor(server).RunTurn(context.Background(), "key", Selection{ID: "grok"}, nil, "prompt", func(Event) error { t.Error("oversized output was emitted"); return nil })
	if err == nil || !strings.Contains(err.Error(), "safety limit") || m.call("Send") != 1 || m.call("CancelRun") != 1 || m.call("CloseAgent") != 1 {
		t.Fatal("runaway output was not cancelled safely", err)
	}
}

func TestNativeToolBlocksAreRejectedButLiteralMarkupIsNotExecuted(t *testing.T) {
	allowed := map[string]bool{"read": true}
	for _, payload := range []string{
		`{"sdkMessage":{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Shell"}]}}}`,
		`{"sdkMessage":{"type":"assistant","message":{"message":{"content":[{"type":"tool_use","name":"foreign__read"}]}}}}`,
		`{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"mcp","args":{"providerIdentifier":"foreign","toolName":"read"}}}}}`,
	} {
		if _, err := classify(json.RawMessage(payload), allowed); err == nil {
			t.Fatal("native execution channel accepted")
		}
	}
	const markup = `<function_calls><invoke name="Shell">not executable</invoke></function_calls>`
	raw, _ := json.Marshal(map[string]any{"sdkMessage": map[string]any{"type": "assistant", "message": map[string]any{"message": map[string]any{"content": []any{map[string]string{"type": "text", "text": markup}}}}}})
	events, err := classify(raw, allowed)
	if err != nil || len(events) != 1 || events[0].Text != markup {
		t.Fatal("literal text must not be interpreted as executable tools", err)
	}
}

func TestPendingCallbacksAreBoundedAndBearerPrefixIsRequired(t *testing.T) {
	server, err := NewToolServer(readTool, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for i := 0; i < maxProposals; i++ {
		server.active <- struct{}{}
	}
	request := httptest.NewRequest(http.MethodPost, callbackPath, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+server.Token)
	response := httptest.NewRecorder()
	server.serve(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("pending callback limit not enforced", response.Code)
	}
	if _, err := server.Results(); err == nil {
		t.Fatal("callback limit violation hidden")
	}
	request.Header.Set("Authorization", "Broken "+server.Token)
	response = httptest.NewRecorder()
	server.serve(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("invalid bearer prefix accepted")
	}
}

func TestToolRequestsListedCountIsNotNativeActivity(t *testing.T) {
	allowed := map[string]bool{"read": true}
	raw := json.RawMessage(`{"interactionUpdate":{"type":"tool-requests-listed","update":{"callCount":1,"type":"tool-requests-listed"}}}`)
	if _, err := classify(raw, allowed); err != nil {
		t.Fatal(err)
	}
	raw = json.RawMessage(`{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"shell"}}}}`)
	if _, err := classify(raw, allowed); err == nil {
		t.Fatal("native shell call accepted")
	}
}

func TestTraceDoesNotChangeBridgeRequestAndReportsWriteErrors(t *testing.T) {
	body := func() string {
		m := &mockBridge{t: t}
		m.onSend = func(_ *mockBridge, w http.ResponseWriter, r *http.Request, _ string) { finished(w, nil) }
		server := m.server()
		defer server.Close()
		if _, err := processFor(server).RunTurn(context.Background(), "key", Selection{ID: "grok"}, nil, "prompt", func(Event) error { return nil }); err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.sendBody
	}
	plain := body()
	t.Setenv(traceEnv, t.TempDir()+"/missing/dir/trace.jsonl")
	if traced := body(); traced != plain || plain == "" {
		t.Fatalf("trace changed request:\n%s\n%s", plain, traced)
	}
}

func TestToolEventFixtures(t *testing.T) {
	allowed := map[string]bool{"read": true}
	cases := []struct {
		name, raw string
		ok        bool
	}{
		{"listed count", `{"interactionUpdate":{"type":"tool-requests-listed","update":{"callCount":1}}}`, true},
		{"discovery", `{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"GetMcpTools"}}}}`, true},
		{"owned custom tool", `{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"mcp","args":{"providerIdentifier":"custom-user-tools","toolName":"read"}}}}}`, true},
		{"undeclared custom tool", `{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"mcp","args":{"providerIdentifier":"custom-user-tools","toolName":"bash"}}}}}`, false},
		{"foreign provider", `{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"mcp","args":{"providerIdentifier":"other","toolName":"read"}}}}}`, false},
		{"native shell", `{"interactionUpdate":{"type":"tool-call-started","update":{"toolCall":{"type":"shell"}}}}`, false},
		{"foreign suffix", `{"sdkMessage":{"type":"tool_call","name":"x__read"}}`, false},
		{"foreign prefix", `{"sdkMessage":{"type":"tool_call","name":"mcp__other__read"}}`, false},
	}
	for _, c := range cases {
		_, err := classify(json.RawMessage(c.raw), allowed)
		if (err == nil) != c.ok {
			t.Errorf("%s: ok=%v err=%v", c.name, c.ok, err)
		}
	}
}
