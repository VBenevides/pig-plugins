package pigtest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// RPCOptions tune one RPC-mode session, which has a UI that can answer dialogs.
type RPCOptions struct {
	// Extensions are paths passed as `-e`.
	Extensions []string
	// Env adds or overrides environment variables.
	Env map[string]string
	// Prompts are sent in order, one at a time. A slash command is answered at once; any other prompt runs the
	// agent loop until the agent settles.
	Prompts []string
	// Confirm answers confirm dialogs; nil declines them.
	Confirm func(request map[string]any) bool
	// Dialog answers select, input and editor dialogs with the response fields (for example {"value": "x"} or
	// {"cancelled": true}); nil cancels them.
	Dialog func(request map[string]any) map[string]any
	// Timeout defaults to 90 seconds for the whole session.
	Timeout time.Duration
}

// RPCResult is everything an RPC session printed.
type RPCResult struct {
	// Events holds every JSON line pig wrote, in order, dialogs included.
	Events []map[string]any
	// Asked holds the dialog requests (confirm, select, input, editor) in order.
	Asked  []map[string]any
	Stderr string
}

// Notices returns the messages of notify requests, in order.
func (r RPCResult) Notices() []string {
	var out []string
	for _, event := range r.Events {
		if event["type"] == "extension_ui_request" && event["method"] == "notify" {
			message, _ := event["message"].(string)
			out = append(out, message)
		}
	}
	return out
}

// Statuses returns the status texts set under key, in order.
func (r RPCResult) Statuses(key string) []string {
	var out []string
	for _, event := range r.Events {
		if event["type"] == "extension_ui_request" && event["method"] == "setStatus" && event["statusKey"] == key {
			text, _ := event["statusText"].(string)
			out = append(out, text)
		}
	}
	return out
}

var dialogMethods = map[string]bool{"confirm": true, "select": true, "input": true, "editor": true}

// RunRPC starts `pig --mode rpc` on provider "mock", model "mock-model", sends each prompt and answers dialogs.
func (h *Home) RunRPC(t testing.TB, mock *MockLLM, opts RPCOptions) RPCResult {
	t.Helper()
	h.WriteModels(t, map[string]ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{"--mode", "rpc", "--provider", "mock", "--model", "mock-model"}
	for _, ext := range opts.Extensions {
		args = append(args, "-e", ext)
	}
	cmd := exec.CommandContext(ctx, "pig", args...)
	cmd.Dir = h.Work
	cmd.Env = h.Env(opts.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr lockedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pig %v: %v", args, err)
	}

	lines := make(chan map[string]any, 256)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				var event map[string]any
				if json.Unmarshal(line, &event) == nil {
					lines <- event
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	var result RPCResult
	send := func(value any) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stdin.Write(append(data, '\n')); err != nil {
			t.Fatalf("write to pig: %v\nstderr:\n%s", err, stderr.String())
		}
	}
	// until reads events, answering dialogs, until done reports true.
	until := func(what string, done func(event map[string]any) bool) {
		for {
			select {
			case event, open := <-lines:
				if !open {
					t.Fatalf("pig RPC session ended before %s\nstderr:\n%s", what, stderr.String())
				}
				result.Events = append(result.Events, event)
				if event["type"] == "extension_ui_request" {
					method, _ := event["method"].(string)
					if dialogMethods[method] {
						result.Asked = append(result.Asked, event)
						send(dialogResponse(event, method, opts))
					}
				}
				if done(event) {
					return
				}
			case <-ctx.Done():
				t.Fatalf("pig RPC session timed out before %s\nstderr:\n%s", what, stderr.String())
			}
		}
	}

	for _, prompt := range opts.Prompts {
		send(map[string]any{"type": "prompt", "message": prompt})
		until("the prompt "+fmt.Sprintf("%q", prompt)+" finished", func(event map[string]any) bool {
			if event["type"] == "agent_settled" {
				return true
			}
			data, _ := event["data"].(map[string]any)
			return event["type"] == "response" && event["command"] == "prompt" && data["disposition"] == "handled"
		})
	}
	// A state round trip guarantees that every earlier event has arrived.
	send(map[string]any{"type": "get_state"})
	until("get_state answered", func(event map[string]any) bool {
		return event["type"] == "response" && event["command"] == "get_state"
	})
	// Events that one extension sends another arrive asynchronously: drain until the stream is quiet.
	for quiet := false; !quiet; {
		select {
		case event, open := <-lines:
			if !open {
				quiet = true
				break
			}
			result.Events = append(result.Events, event)
		case <-time.After(400 * time.Millisecond):
			quiet = true
		}
	}

	_ = stdin.Close()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-waited
	}
	result.Stderr = stderr.String()
	return result
}

func dialogResponse(request map[string]any, method string, opts RPCOptions) map[string]any {
	response := map[string]any{"type": "extension_ui_response", "id": request["id"]}
	if method == "confirm" {
		response["confirmed"] = opts.Confirm != nil && opts.Confirm(request)
		return response
	}
	fields := map[string]any{"cancelled": true}
	if opts.Dialog != nil {
		fields = opts.Dialog(request)
	}
	for key, value := range fields {
		response[key] = value
	}
	return response
}

// lockedBuffer is a bytes.Buffer that the exec package and the test can use from different goroutines.
type lockedBuffer struct {
	mu   sync.Mutex
	data []byte
}

var _ io.Writer = (*lockedBuffer)(nil)

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
