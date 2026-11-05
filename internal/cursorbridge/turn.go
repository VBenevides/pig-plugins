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
	defer func() {
		// Teardown must not depend on the cancelled request context.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var failures []error
		if agentID != "" {
			if closeErr := p.Client.CloseAgent(cleanup, agentID); closeErr != nil {
				failures = append(failures, fmt.Errorf("close Cursor agent: %w", closeErr))
			}
		}
		if clearErr := p.Client.SetToolCallback(cleanup, "", ""); clearErr != nil {
			failures = append(failures, fmt.Errorf("clear Cursor tool callback: %w", clearErr))
		}
		if closeErr := server.Close(); closeErr != nil {
			failures = append(failures, fmt.Errorf("stop Cursor tool callback server: %w", closeErr))
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
	result, sendErr := p.Client.Send(runCtx, agentID, prompt, func(data json.RawMessage) error {
		events, violation := classify(data, allowed)
		if violation != nil {
			server.Fail(violation)
			cancelRun()
			return violation
		}
		for _, event := range events {
			streamed = true
			if emitErr := emit(event); emitErr != nil {
				return emitErr
			}
		}
		return nil
	})
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
		if item != nil && strings.Contains(strings.ToLower(item.Type), "tool") {
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

// offered reports whether a tool event names one of PiG's tools. A missing name
// is not offered.
func offered(payload json.RawMessage, allowed map[string]bool) bool {
	var fields struct {
		Name     string `json:"name"`
		ToolName string `json:"toolName"`
		ToolCall struct {
			Name string `json:"name"`
		} `json:"toolCall"`
	}
	if json.Unmarshal(payload, &fields) != nil {
		return false
	}
	for _, name := range []string{fields.Name, fields.ToolName, fields.ToolCall.Name} {
		if name == "" {
			continue
		}
		if allowed[name] {
			return true
		}
		for _, separator := range []string{"__", ":", "."} {
			if i := strings.LastIndex(name, separator); i >= 0 && allowed[name[i+len(separator):]] {
				return true
			}
		}
		return false
	}
	return false
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
