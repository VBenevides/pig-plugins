package cursorbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// grace lets sibling parallel tool calls arrive before the run is cancelled.
const grace = 100 * time.Millisecond

// A local guard against runaway generations. This is not a billing/token cap.
const maxTurnOutputBytes = 256 << 10

// Event is incremental model output.
type Event struct {
	Thinking bool
	Text     string
}

// Outcome is the result of one turn. Proposals are tool calls for PiG to run;
// the turn ends after them. CleanupError reports failed teardown, which never
// discards a finished generation but must stay visible.
type Outcome struct {
	Proposals    []Proposal
	Usage        *TokenUsage
	Text         string
	CleanupError error
}

// RunTurn runs one generation on a fresh agent. History is rendered by the caller
// into prompt, so no remote conversation outlives the turn and no account or
// branch can observe another's state. A Process runs one turn at a time.
func (p *Process) RunTurn(ctx context.Context, key string, model Selection, tools []ToolDef, prompt string, emit func(Event) error) (out Outcome, err error) {
	p.turn.Lock()
	defer p.turn.Unlock()
	if p.Client == nil {
		return out, errors.New("Cursor bridge is not running")
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var handedOff atomic.Bool
	server, err := NewToolServer(tools, func() {
		time.AfterFunc(grace, func() { handedOff.Store(true); cancelRun() })
	})
	if err != nil {
		return out, err
	}
	var agentID string
	var result RunResult
	defer func() {
		cancelRun()
		// Teardown must not depend on the cancelled request context.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var failures []error
		// Release parked HTTP callbacks before agent teardown can wait on them.
		if closeErr := server.Close(); closeErr != nil {
			failures = append(failures, fmt.Errorf("stop Cursor tool callback server: %w", closeErr))
		}
		if agentID != "" && result.RunID != "" && result.Status != "RUN_LIFECYCLE_STATUS_FINISHED" {
			if cancelErr := p.Client.Cancel(cleanup, agentID, result.RunID); cancelErr != nil {
				failures = append(failures, fmt.Errorf("cancel Cursor run: %w", cancelErr))
			}
		}
		if agentID != "" {
			if closeErr := p.Client.CloseAgent(cleanup, agentID); closeErr != nil {
				failures = append(failures, fmt.Errorf("close Cursor agent: %w", closeErr))
			}
		}
		if clearErr := p.Client.SetToolCallback(cleanup, "", ""); clearErr != nil {
			failures = append(failures, fmt.Errorf("clear Cursor tool callback: %w", clearErr))
		}
		out.CleanupError = errors.Join(failures...)
	}()
	if err = p.Client.SetToolCallback(runCtx, server.URL, server.Token); err != nil {
		return out, fmt.Errorf("register Cursor tool callback: %w", err)
	}
	if agentID, err = p.Client.Create(runCtx, key, model, tools); err != nil {
		return out, fmt.Errorf("create Cursor agent: %w", err)
	}
	server.Expect(agentID)

	allowed := map[string]bool{}
	for _, tool := range tools {
		allowed[tool.Name] = true
	}
	streamed := false
	outputBytes := 0
	result, sendErr := p.Client.Send(runCtx, agentID, prompt, func(data json.RawMessage) error {
		traceEvent(data)
		events, violation := classify(data, allowed)
		if violation != nil {
			server.Fail(violation)
			cancelRun()
			return violation
		}
		for _, event := range events {
			if len(event.Text) > maxTurnOutputBytes-outputBytes {
				return errors.New("Cursor output exceeded the per-turn safety limit; no retry was attempted")
			}
			outputBytes += len(event.Text)
			if !event.Thinking && event.Text != "" {
				streamed = true
			}
			if emitErr := emit(event); emitErr != nil {
				return emitErr
			}
		}
		return nil
	})
	traceCallbacks(server)
	proposals, violation := server.Results()
	switch {
	case violation != nil:
		return out, violation
	case ctx.Err() != nil:
		return out, ctx.Err()
	case handedOff.Load() && len(proposals) > 0:
		// The cancel that ended this run was ours; the proposals are the result.
		out.Proposals = proposals
		return out, nil
	case sendErr != nil:
		return out, fmt.Errorf("send Cursor turn: %w", sendErr)
	case len(proposals) > 0:
		out.Proposals = proposals
		return out, nil
	}
	if len(result.Result.Text) > maxTurnOutputBytes || (!streamed && len(result.Result.Text) > maxTurnOutputBytes-outputBytes) {
		return out, errors.New("Cursor final output exceeded the per-turn safety limit; no retry was attempted")
	}
	out.Text, out.Usage = result.Result.Text, result.Result.Usage
	if !streamed && out.Text != "" {
		if err = emit(Event{Text: out.Text}); err != nil {
			return out, err
		}
	}
	return out, nil
}

// classify extracts model output from a stream event and refuses any tool
// activity that PiG did not offer. Unknown tool shapes fail closed.
func classify(data json.RawMessage, allowed map[string]bool) ([]Event, error) {
	var envelope struct {
		SDKMessage        *typed `json:"sdkMessage"`
		InteractionUpdate *typed `json:"interactionUpdate"`
		Step              *typed `json:"step"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode Cursor stream event: %w", err)
	}
	for _, item := range []*typed{envelope.InteractionUpdate, envelope.Step} {
		if item != nil && item.Type != "tool-requests-listed" && strings.Contains(strings.ToLower(item.Type), "tool") {
			// tool-requests-listed only carries a call count, never a tool name.
			if !offered(item.payload(), allowed) {
				return nil, errors.New("Cursor reported native tool activity; refusing to continue")
			}
		}
	}
	message := envelope.SDKMessage
	if message == nil {
		return nil, nil
	}
	kind := strings.ToLower(message.Type)
	payload := message.payload()
	switch {
	case strings.Contains(kind, "tool"):
		if !offered(payload, allowed) {
			return nil, errors.New("Cursor reported native tool activity; refusing to continue")
		}
		return nil, nil
	case kind == "assistant" || kind == "thinking":
		if err := checkContentTools(payload, allowed); err != nil {
			return nil, err
		}
		return blocks(kind == "thinking", payload), nil
	}
	return nil, nil
}

type typed struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
	Update  json.RawMessage `json:"update"`
	Step    json.RawMessage `json:"step"`
}

func (t *typed) payload() json.RawMessage {
	for _, candidate := range []json.RawMessage{t.Message, t.Update, t.Step} {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

// offered accepts only the SDK's owned custom-tool namespace. Matching a suffix
// such as foreign__read would authorize an unrelated execution channel.
func offered(payload json.RawMessage, allowed map[string]bool) bool {
	if len(allowed) == 0 {
		return false
	}
	type toolIdentity struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Args struct {
			Provider string `json:"providerIdentifier"`
			Tool     string `json:"toolName"`
		} `json:"args"`
		ToolCall json.RawMessage `json:"toolCall"`
	}
	var fields toolIdentity
	if json.Unmarshal(payload, &fields) != nil {
		return false
	}
	if len(fields.ToolCall) > 0 {
		var nested toolIdentity
		if json.Unmarshal(fields.ToolCall, &nested) != nil || len(nested.ToolCall) > 0 {
			return false
		}
		fields = nested
	}
	name := fields.Name
	if name == "" {
		name = fields.Type
	}
	if name == "GetMcpTools" {
		// Metadata discovery only. There are no external MCP servers or settings.
		return true
	}
	if name == "mcp" || name == "CallMcpTool" {
		return fields.Args.Provider == "custom-user-tools" && allowed[fields.Args.Tool]
	}
	const prefix = "mcp__custom-user-tools__"
	return strings.HasPrefix(name, prefix) && allowed[strings.TrimPrefix(name, prefix)]
}

func checkContentTools(payload json.RawMessage, allowed map[string]bool) error {
	var message struct {
		Content []json.RawMessage `json:"content"`
		Message struct {
			Content []json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(payload, &message) != nil {
		return errors.New("Cursor sent malformed assistant content")
	}
	for _, block := range append(message.Content, message.Message.Content...) {
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(block, &kind) != nil {
			return errors.New("Cursor sent malformed assistant content")
		}
		if kind.Type == "tool_use" && !offered(block, allowed) {
			return errors.New("Cursor reported native tool activity; refusing to continue")
		}
	}
	return nil
}

func blocks(thinking bool, payload json.RawMessage) []Event {
	var message struct {
		Text    string `json:"text"`
		Message struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"content"`
		} `json:"message"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
	}
	if json.Unmarshal(payload, &message) != nil {
		return nil
	}
	var events []Event
	if message.Text != "" {
		events = append(events, Event{Thinking: thinking, Text: message.Text})
	}
	for _, content := range append(message.Content, message.Message.Content...) {
		switch {
		case content.Type == "thinking" || thinking:
			if text := content.Thinking + content.Text; text != "" {
				events = append(events, Event{Thinking: true, Text: text})
			}
		case content.Type == "text" && content.Text != "":
			events = append(events, Event{Text: content.Text})
		}
	}
	return events
}
