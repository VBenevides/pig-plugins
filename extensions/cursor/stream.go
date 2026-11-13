package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
	"github.com/VBenevides/pig-plugins/internal/cursormodels"
)

func (s *service) stream(model, request map[string]any, options sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error) {
	key, _ := options.Values["apiKey"].(string)
	if key == "" {
		return nil, errors.New("cursor API key is missing; run /login and select a Cursor account")
	}
	modelID, _ := model["id"].(string)
	level, _ := options.Values["reasoning"].(string)
	prompt, images, err := Render(request)
	if err != nil {
		return nil, err
	}
	tools, err := requestTools(request)
	if err != nil {
		return nil, err
	}
	requestKeys := make([]string, 0, len(request))
	for key := range request {
		requestKeys = append(requestKeys, key)
	}
	cursorbridge.TraceRequestShape(requestKeys, len(tools))
	parent := options.Signal
	if parent == nil {
		parent = context.Background()
	}
	stream := sdk.CreateAssistantMessageEventStream()
	go func() {
		ctx, cancel := context.WithTimeout(parent, turnTimeout)
		defer cancel()
		out := &turnWriter{stream: stream, msg: map[string]any{
			"role": "assistant", "api": API, "provider": Provider, "model": modelID,
			"timestamp": time.Now().UnixMilli(), "stopReason": "stop", "content": []any{},
			// Replay could repeat a remote generation, so the host must not retry it.
			"replayProhibited": true,
		}}
		err := s.runTurn(ctx, out, key, modelID, level, request, tools, prompt, images)
		if err != nil {
			reason := "error"
			if ctx.Err() != nil {
				reason = "aborted"
			}
			out.msg["stopReason"] = reason
			out.msg["errorMessage"] = err.Error()
			stream.Push(map[string]any{"type": "error", "reason": reason, "error": out.snapshot()})
			return
		}
		stream.Push(map[string]any{"type": "done", "reason": out.msg["stopReason"], "message": out.snapshot()})
	}()
	return stream, nil
}

func (s *service) runTurn(ctx context.Context, out *turnWriter, key, modelID, level string, request map[string]any, tools []cursorbridge.ToolDef, prompt string, images []cursorbridge.Image) error {
	entries, err := s.entriesFor(ctx, key)
	if err != nil {
		return err
	}
	selection, err := cursormodels.Resolve(entries, modelID, level)
	if err != nil {
		return err
	}
	r, release, err := s.manager.acquire(ctx, key)
	if err != nil {
		return err
	}
	pinned := false // a parked run keeps the process until it ends
	defer func() {
		if !pinned {
			release()
		}
	}()
	if err = out.start(); err != nil {
		return err
	}
	account := accountHash(key)
	var outcome cursorbridge.Outcome
	var run parkedRun
	resumed := false
	if parkingEnabled() {
		outcome, resumed, err = s.tryResume(ctx, out, account, modelID, level, request, tools)
	}
	if !resumed && err == nil {
		var opts cursorbridge.TurnOptions
		opts.Park = parkingEnabled() && len(tools) > 0
		outcome, run, err = r.Run(ctx, key, selection, tools, prompt, out.event, opts, images...)
	}
	defer func() {
		// A parked run that was never registered would wait for its timeout.
		if run != nil && !pinned {
			_ = run.Close()
		}
	}()
	if outcome.CleanupError != nil {
		// The generation finished or failed on its own terms; teardown trouble stays visible.
		out.msg["cleanupWarning"] = outcome.CleanupError.Error()
	}
	if err != nil {
		return err
	}
	if err = out.finishBlock(); err != nil {
		return err
	}
	if len(outcome.Proposals) > 0 {
		out.msg["stopReason"] = "toolUse"
		for i, proposal := range outcome.Proposals {
			if err = out.toolCall(i, proposal); err != nil {
				return err
			}
		}
	}
	if run != nil && len(outcome.Proposals) > 0 {
		if fp, ok := fingerprint(account, modelID, level, tools, request, messageCount(request)); ok {
			s.parked.put(account, &parkedEntry{run: run, fingerprint: fp, release: release})
			pinned = true
		} else {
			_ = run.Close()
		}
	}
	s.usage.add(modelKey(modelID, level), outcome)
	if usage := outcome.Usage; usage != nil {
		out.msg["usage"] = piUsage(*usage)
	}
	return nil
}

// requestTools returns the tools in force for this request. PiG carries tool
// declarations inside system messages (toolsAdded/toolsRemoved), replayed in
// order. A top-level "tools" array is still honored for older hosts.
func requestTools(request map[string]any) ([]cursorbridge.ToolDef, error) {
	order := []string{}
	byName := map[string]any{}
	add := func(items any) error {
		list, _ := items.([]any)
		inList := map[string]bool{}
		for _, item := range list {
			tool, ok := item.(map[string]any)
			if !ok {
				return errors.New("cursor tool definition is invalid")
			}
			name, _ := tool["name"].(string)
			if name == "" {
				return errors.New("cursor tool name is missing or duplicated")
			}
			if inList[name] {
				return errors.New("cursor tool name is missing or duplicated")
			}
			inList[name] = true
			if _, exists := byName[name]; !exists {
				order = append(order, name)
			}
			byName[name] = tool
		}
		return nil
	}
	if err := add(request["tools"]); err != nil {
		return nil, err
	}
	messages, _ := request["messages"].([]any)
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok || message["role"] != "system" {
			continue
		}
		removed, _ := message["toolsRemoved"].([]any)
		for _, entry := range removed {
			ref, _ := entry.(map[string]any)
			name, _ := ref["name"].(string)
			if _, exists := byName[name]; exists {
				delete(byName, name)
				kept := order[:0]
				for _, existing := range order {
					if existing != name {
						kept = append(kept, existing)
					}
				}
				order = kept
			}
		}
		if err := add(message["toolsAdded"]); err != nil {
			return nil, err
		}
	}
	current := make([]any, 0, len(order))
	for _, name := range order {
		current = append(current, byName[name])
	}
	return toolDefs(current)
}

// toolDefs converts PiG tool declarations into Cursor custom-tool definitions.
func toolDefs(value any) ([]cursorbridge.ToolDef, error) {
	items, _ := value.([]any)
	seen := map[string]bool{}
	var tools []cursorbridge.ToolDef
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("cursor tool definition is invalid")
		}
		name, _ := tool["name"].(string)
		if name == "" || seen[name] {
			return nil, errors.New("cursor tool name is missing or duplicated")
		}
		seen[name] = true
		description, _ := tool["description"].(string)
		schema, err := json.Marshal(tool["parameters"])
		if err != nil {
			return nil, fmt.Errorf("encode schema for tool %q: %w", name, err)
		}
		if string(schema) == "null" {
			schema = nil
		}
		tools = append(tools, cursorbridge.ToolDef{Name: name, Description: description, Schema: schema})
	}
	return tools, nil
}

// turnWriter turns bridge events into host events. All calls come from one goroutine.
type turnWriter struct {
	stream *sdk.ModelEventStream
	msg    map[string]any
	// open is the index and kind of the block being streamed, or -1.
	open     int
	openKind string
	openText string
	started  bool
}

// snapshot copies the message, so an event queued earlier never changes later.
func (w *turnWriter) snapshot() map[string]any {
	data, err := json.Marshal(w.msg)
	if err != nil {
		return map[string]any{"role": "assistant", "stopReason": "error", "errorMessage": "encode Cursor message: " + err.Error()}
	}
	var copy map[string]any
	_ = json.Unmarshal(data, &copy)
	return copy
}

func (w *turnWriter) start() error {
	w.open = -1
	w.started = true
	w.stream.Push(map[string]any{"type": "start", "partial": w.snapshot()})
	return nil
}

func (w *turnWriter) content() []any { c, _ := w.msg["content"].([]any); return c }

func (w *turnWriter) event(e cursorbridge.Event) error {
	if e.Text == "" {
		return nil
	}
	kind := "text"
	if e.Thinking {
		kind = "thinking"
	}
	if w.open >= 0 && w.openKind != kind {
		if err := w.finishBlock(); err != nil {
			return err
		}
	}
	if w.open < 0 {
		w.open, w.openKind, w.openText = len(w.content()), kind, ""
		field := "text"
		if kind == "thinking" {
			field = "thinking"
		}
		w.msg["content"] = append(w.content(), map[string]any{"type": kind, field: ""})
		w.stream.Push(map[string]any{"type": kind + "_start", "contentIndex": w.open, "partial": w.snapshot()})
	}
	w.openText += e.Text
	w.setOpenText()
	w.stream.Push(map[string]any{"type": kind + "_delta", "contentIndex": w.open, "delta": e.Text, "partial": w.snapshot()})
	return nil
}

func (w *turnWriter) setOpenText() {
	field := "text"
	if w.openKind == "thinking" {
		field = "thinking"
	}
	w.content()[w.open].(map[string]any)[field] = w.openText
}

func (w *turnWriter) finishBlock() error {
	if w.open < 0 {
		return nil
	}
	index, kind := w.open, w.openKind
	w.open = -1
	w.stream.Push(map[string]any{"type": kind + "_end", "contentIndex": index, "partial": w.snapshot()})
	return nil
}

func (w *turnWriter) toolCall(ordinal int, proposal cursorbridge.Proposal) error {
	var arguments any
	if err := json.Unmarshal(proposal.Args, &arguments); err != nil {
		return fmt.Errorf("cursor sent invalid arguments for tool %q: %w", proposal.Name, err)
	}
	if _, ok := arguments.(map[string]any); !ok {
		return fmt.Errorf("cursor sent non-object arguments for tool %q", proposal.Name)
	}
	id := proposal.ID
	if id == "" {
		id = "cursor_" + strconv.Itoa(ordinal) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	call := map[string]any{"type": "toolCall", "id": id, "name": proposal.Name, "arguments": arguments}
	index := len(w.content())
	w.msg["content"] = append(w.content(), call)
	w.stream.Push(map[string]any{"type": "toolcall_start", "contentIndex": index, "partial": w.snapshot()})
	w.stream.Push(map[string]any{"type": "toolcall_end", "contentIndex": index, "toolCall": call, "partial": w.snapshot()})
	return nil
}
