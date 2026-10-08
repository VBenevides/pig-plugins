// Package pigtest runs the real `pig` binary against a scripted model inside an isolated HOME, so extension
// tests observe behavior through PiG itself.
package pigtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// ToolCall is one tool call in a scripted model reply.
type ToolCall struct {
	Name string
	Args any
}

// Reply is one scripted model answer. Set exactly one of Text, Calls or Status.
type Reply struct {
	Text  string
	Calls []ToolCall
	// Status > 0 answers with that HTTP status and an OpenAI-style error body carrying Message.
	Status  int
	Message string
	// PromptTokens is the usage the reply reports (default 10); raise it to trigger auto-compaction.
	PromptTokens int
	// Delay is waited before answering.
	Delay time.Duration
	// Chunks streams these text pieces before the final reply, ChunkDelay apart.
	Chunks     []string
	ChunkDelay time.Duration
}

// Text is a plain assistant reply.
func Text(s string) Reply { return Reply{Text: s} }

// Calls is an assistant reply that calls tools.
func Calls(calls ...ToolCall) Reply { return Reply{Calls: calls} }

// Call builds one ToolCall.
func Call(name string, args any) ToolCall { return ToolCall{Name: name, Args: args} }

// HTTPError answers with an HTTP error status.
func HTTPError(status int, message string) Reply { return Reply{Status: status, Message: message} }

// AnyModel is the script key that serves models without a script of their own.
const AnyModel = "*"

// MockLLM is a scripted OpenAI-compatible chat-completions server. Each request pops the next reply from the
// script of the requested model (or AnyModel); an exhausted script answers "done".
type MockLLM struct {
	// URL is the base URL for a provider entry (ends in /v1).
	URL string

	server   *httptest.Server
	mu       sync.Mutex
	script   map[string][]Reply
	requests []map[string]any
}

// NewMockLLM serves the same script for every model.
func NewMockLLM(script ...Reply) *MockLLM {
	return NewMockLLMByModel(map[string][]Reply{AnyModel: script})
}

// NewMockLLMByModel serves one script per model id; AnyModel serves models that have none.
func NewMockLLMByModel(script map[string][]Reply) *MockLLM {
	m := &MockLLM{script: map[string][]Reply{}}
	for model, replies := range script {
		m.script[model] = append([]Reply(nil), replies...)
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	m.URL = m.server.URL + "/v1"
	return m
}

// Close stops the server.
func (m *MockLLM) Close() { m.server.Close() }

// Requests returns the decoded request bodies in arrival order.
func (m *MockLLM) Requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.requests...)
}

func (m *MockLLM) handle(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "mockllm: invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.requests = append(m.requests, body)
	call := len(m.requests)
	model, _ := body["model"].(string)
	queue, ok := m.script[model]
	key := model
	if !ok {
		queue, key = m.script[AnyModel], AnyModel
	}
	reply := Text("done")
	if len(queue) > 0 {
		reply = queue[0]
		m.script[key] = queue[1:]
	}
	m.mu.Unlock()

	if reply.Delay > 0 {
		time.Sleep(reply.Delay)
	}
	if reply.Status > 0 {
		message := reply.Message
		if message == "" {
			message = "mock error"
		}
		payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": "mock_error"}})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.Status)
		w.Write(payload)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	send := func(chunk any) {
		payload, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", payload)
	}
	for _, piece := range reply.Chunks {
		send(map[string]any{"choices": []any{map[string]any{"index": 0,
			"delta": map[string]any{"role": "assistant", "content": piece}, "finish_reason": nil}}})
		flush()
		time.Sleep(reply.ChunkDelay)
	}

	delta := map[string]any{"role": "assistant", "content": reply.Text}
	finish := "stop"
	if len(reply.Calls) > 0 {
		calls := make([]any, len(reply.Calls))
		for i, c := range reply.Calls {
			args, _ := json.Marshal(c.Args)
			calls[i] = map[string]any{"index": i, "id": fmt.Sprintf("call_%d_%d", call, i), "type": "function",
				"function": map[string]any{"name": c.Name, "arguments": string(args)}}
		}
		delta = map[string]any{"role": "assistant", "tool_calls": calls}
		finish = "tool_calls"
	}
	promptTokens := reply.PromptTokens
	if promptTokens == 0 {
		promptTokens = 10
	}
	send(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
	send(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	send(map[string]any{"choices": []any{}, "usage": map[string]any{
		"prompt_tokens": promptTokens, "completion_tokens": 5, "total_tokens": promptTokens + 5}})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush()
}

// ToolNames lists the tool names offered in the first request.
func ToolNames(m *MockLLM) []string {
	requests := m.Requests()
	if len(requests) == 0 {
		return nil
	}
	var names []string
	tools, _ := requests[0]["tools"].([]any)
	for _, t := range tools {
		fn, _ := t.(map[string]any)["function"].(map[string]any)
		if name, ok := fn["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// ToolResults returns the content of every tool-result message in the final request, in order.
func ToolResults(m *MockLLM) []string {
	requests := m.Requests()
	if len(requests) == 0 {
		return nil
	}
	var results []string
	messages, _ := requests[len(requests)-1]["messages"].([]any)
	for _, raw := range messages {
		msg, _ := raw.(map[string]any)
		if msg["role"] != "tool" {
			continue
		}
		switch content := msg["content"].(type) {
		case string:
			results = append(results, content)
		default:
			encoded, _ := json.Marshal(content)
			results = append(results, string(encoded))
		}
	}
	return results
}
