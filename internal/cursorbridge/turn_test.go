package cursorbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockBridge struct {
	t         *testing.T
	mu        sync.Mutex
	calls     []string
	callback  string
	token     string
	created   map[string]json.RawMessage
	onSend    func(m *mockBridge, w http.ResponseWriter, r *http.Request, agentID string)
	cancelled bool
}

func (m *mockBridge) call(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if strings.HasSuffix(c, name) {
			n++
		}
	}
	return n
}

func (m *mockBridge) post(path string, body any) (int, string) {
	data, _ := json.Marshal(body)
	m.mu.Lock()
	callback, token := m.callback, m.token
	m.mu.Unlock()
	req, _ := http.NewRequest(http.MethodPost, callback+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		m.t.Error(err)
		return 0, ""
	}
	defer response.Body.Close()
	out, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(out)
}

func (m *mockBridge) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.calls = append(m.calls, r.URL.Path)
		m.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "SetToolCallback"):
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			m.mu.Lock()
			m.callback, m.token = in["url"], in["authToken"]
			m.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "CreateAgent"):
			var in struct {
				Options map[string]json.RawMessage `json:"options"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			m.created = in.Options
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"agentId":"agent-1"}`)
		case strings.HasSuffix(r.URL.Path, "CancelRun"), strings.HasSuffix(r.URL.Path, "CloseAgent"):
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "SdkAgentService/Send"):
			// Reading the body lets the server notice a client disconnect.
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/connect+json")
			m.onSend(m, w, r, "agent-1")
		default:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
		}
	}))
}

func finished(w http.ResponseWriter, usage map[string]any) {
	result := map[string]any{"result": "final answer"}
	if usage != nil {
		result["usage"] = usage
	}
	frame(w, 0, map[string]any{"result": map[string]any{"agentId": "agent-1", "runId": "r", "status": "RUN_LIFECYCLE_STATUS_FINISHED", "result": result}})
	frame(w, 2, map[string]any{})
}

func processFor(server *httptest.Server) *Process {
	c, _ := NewClient(server.URL, "bridge-token")
	return &Process{Client: c}
}

var readTool = []ToolDef{{Name: "read", Description: "read a file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}}

func TestTurnReturnsProposalWithoutExecuting(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		status, body := m.post(callbackPath, map[string]any{"agentId": agent, "toolName": "read", "toolCallId": "c1", "args": map[string]string{"path": "a.go"}})
		if status != http.StatusConflict || strings.Contains(body, "handed off") {
			t.Errorf("callback %d %s", status, body)
		}
		// A real run is cancelled by the adapter; wait for that.
		<-r.Context().Done()
	}
	server := m.server()
	defer server.Close()
	out, err := processFor(server).RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Proposals) != 1 || out.Proposals[0].Name != "read" || string(out.Proposals[0].Args) != `{"path":"a.go"}` {
		t.Fatalf("proposals %+v", out.Proposals)
	}
	if string(m.created["tools"]) != `{"names":["mcp"]}` || string(m.created["mcpServers"]) != `{}` {
		t.Errorf("unsafe tool channel configuration: %+v", m.created)
	}
	if m.call("CloseAgent") != 1 || out.CleanupError != nil {
		t.Errorf("cleanup: %d %v", m.call("CloseAgent"), out.CleanupError)
	}
	if m.call("CancelRun") > 1 {
		t.Error("run cancelled repeatedly")
	}
}

func TestUndeclaredToolsAreRefused(t *testing.T) {
	cases := map[string]func(m *mockBridge, w http.ResponseWriter, agent string){
		"callback for an unoffered tool": func(m *mockBridge, w http.ResponseWriter, agent string) {
			if status, _ := m.post(callbackPath, map[string]any{"agentId": agent, "toolName": "bash", "args": map[string]string{"command": "id"}}); status != 403 {
				t.Errorf("status %d", status)
			}
			finished(w, nil)
		},
		"callback from another agent": func(m *mockBridge, w http.ResponseWriter, agent string) {
			m.post(callbackPath, map[string]any{"agentId": "other", "toolName": "read"})
			finished(w, nil)
		},
		"native shell event": func(m *mockBridge, w http.ResponseWriter, agent string) {
			frame(w, 0, map[string]any{"sdkMessage": map[string]any{"type": "tool_call", "message": map[string]any{"name": "shell"}}})
			finished(w, nil)
		},
		"native tool without a name": func(m *mockBridge, w http.ResponseWriter, agent string) {
			frame(w, 0, map[string]any{"interactionUpdate": map[string]any{"type": "tool-call-started"}})
			finished(w, nil)
		},
	}
	for name, send := range cases {
		t.Run(name, func(t *testing.T) {
			m := &mockBridge{t: t}
			m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) { send(m, w, agent) }
			server := m.server()
			defer server.Close()
			out, err := processFor(server).RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil })
			if err == nil || len(out.Proposals) != 0 {
				t.Fatalf("violation accepted: %v %+v", err, out)
			}
		})
	}
}

func TestCallbackRequiresBearerToken(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		saved := m.token
		m.token = "wrong"
		if status, _ := m.post(callbackPath, map[string]any{"agentId": agent, "toolName": "read"}); status != 401 {
			t.Errorf("wrong token status %d", status)
		}
		m.token = ""
		if status, _ := m.post(callbackPath, map[string]any{"agentId": agent, "toolName": "read"}); status != 401 {
			t.Errorf("missing token status %d", status)
		}
		m.token = saved
		finished(w, map[string]any{"inputTokens": "10", "outputTokens": "5", "totalTokens": "15"})
	}
	server := m.server()
	defer server.Close()
	var seen []Event
	out, err := processFor(server).RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(e Event) error { seen = append(seen, e); return nil })
	if err != nil || len(out.Proposals) != 0 {
		t.Fatalf("%v %+v", err, out)
	}
	if out.Usage == nil || out.Usage.InputTokens != 10 || out.Usage.TotalTokens != 15 {
		t.Fatalf("usage %+v", out.Usage)
	}
	if len(seen) != 1 || seen[0].Text != "final answer" {
		t.Fatalf("events %+v", seen)
	}
}

func TestDuplicateAndParallelProposals(t *testing.T) {
	proposed := make(chan struct{}, 3)
	callback, err := NewToolServer(readTool, func() { proposed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Close()
	callback.Expect("agent")
	m := &mockBridge{t: t, callback: callback.URL, token: callback.Token}
	var pending sync.WaitGroup
	send := func(id, path string) {
		pending.Add(1)
		go func() {
			defer pending.Done()
			m.post(callbackPath, map[string]any{"agentId": "agent", "toolName": "read", "toolCallId": id, "args": map[string]string{"path": path}})
		}()
	}
	send("a", "1")
	select {
	case <-proposed:
	case <-time.After(time.Second):
		t.Fatal("first proposal did not arrive")
	}
	send("a", "1") // identical retry remains parked and is deduplicated
	send("b", "2")
	select {
	case <-proposed:
	case <-time.After(time.Second):
		t.Fatal("parallel proposal did not arrive")
	}
	status, _ := m.post(callbackPath, map[string]any{"agentId": "agent", "toolName": "read", "toolCallId": "a", "args": map[string]string{"path": "other"}})
	if status != http.StatusForbidden {
		t.Fatalf("conflicting duplicate status %d", status)
	}
	proposals, violation := callback.Results()
	if len(proposals) != 2 || violation == nil {
		t.Fatal("duplicate handling failed", proposals, violation)
	}
	if err := callback.Close(); err != nil {
		t.Fatal(err)
	}
	pending.Wait()

	m2 := &mockBridge{t: t}
	m2.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		var pending sync.WaitGroup
		for _, id := range []string{"a", "a", "b"} {
			pending.Add(1)
			go func(id string) {
				defer pending.Done()
				m.post(callbackPath, map[string]any{"agentId": agent, "toolName": "read", "toolCallId": id, "args": map[string]string{"path": id}})
			}(id)
		}
		<-r.Context().Done()
		pending.Wait()
	}
	server2 := m2.server()
	defer server2.Close()
	out, err := processFor(server2).RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil })
	if err != nil || len(out.Proposals) != 2 {
		t.Fatalf("%v %+v", err, out.Proposals)
	}
}

func TestCancellationAndTeardown(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	server := m.server()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := processFor(server).RunTurn(ctx, "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil })
	if err == nil {
		t.Fatal("cancelled turn succeeded")
	}
	if m.call("CloseAgent") != 1 {
		t.Errorf("agent not closed after cancel: %d", m.call("CloseAgent"))
	}
	if m.token != "" && m.callback == "" {
		t.Error("inconsistent callback state")
	}
	if m.call("SetToolCallback") != 2 {
		t.Errorf("callback was not cleared: %d", m.call("SetToolCallback"))
	}
}

func TestTurnsAreSerialized(t *testing.T) {
	m := &mockBridge{t: t}
	var active, peak int
	var mu sync.Mutex
	m.onSend = func(m *mockBridge, w http.ResponseWriter, r *http.Request, agent string) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		finished(w, nil)
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.RunTurn(context.Background(), "account-key", Selection{ID: "grok"}, readTool, "prompt", func(Event) error { return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Fatalf("concurrent turns shared one bridge: %d", peak)
	}
}
