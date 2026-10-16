package cursordirect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

const handoff = "Tool call received and handed off to the external client for execution. Do not retry or call it again; end the turn. The result will be provided in the next request."

// Stream emits native events. Tools are proposals only: OMP's external-client
// handoff ends the remote turn; PiG executes through its normal approval loop,
// and its next transcript supplies the actual results. No tool executes here.
func (c *Client) Stream(model, request map[string]any, options sdk.ProviderStreamOptions) (*sdk.ModelEventStream, error) {
	for _, key := range []string{"temperature", "topP", "topK", "seed", "toolChoice", "reasoningEffort", "thinkingBudgets", "thinkingBudgetTokens"} {
		if v := options.Values[key]; v != nil {
			return nil, fmt.Errorf("cursor: unsupported setting %s; select an exact discovered model variant", key)
		}
	}
	h, run, err := buildHistory(model, request)
	if err != nil {
		return nil, err
	}
	token := text(options.Values["apiKey"])
	if token == "" {
		if auth, ok := options.Values["auth"].(map[string]any); ok {
			token = text(auth["apiKey"])
		}
	}
	if token == "" {
		return nil, errors.New("cursor: no selected-account access token; run /login cursor")
	}
	parent := options.Signal
	if parent == nil {
		parent = context.Background()
	}
	stream := sdk.CreateAssistantMessageEventStream()
	go func() {
		ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
		defer cancel()
		msg := map[string]any{"role": "assistant", "api": "cursor-agent", "provider": "cursor", "model": model["id"], "timestamp": time.Now().UnixMilli(), "stopReason": "stop", "content": []any{}, "replayProhibited": true}
		err := c.run(ctx, token, h, run, msg, stream)
		if err != nil {
			msg["stopReason"] = "error"
			if ctx.Err() != nil {
				msg["stopReason"] = "aborted"
			}
			msg["errorMessage"] = err.Error()
			stream.Push(map[string]any{"type": "error", "reason": msg["stopReason"], "error": msg})
			return
		}
		stream.Push(map[string]any{"type": "done", "reason": msg["stopReason"], "message": msg})
	}()
	return stream, nil
}
func (c *Client) run(ctx context.Context, token string, h *history, run []byte, msg map[string]any, stream *sdk.ModelEventStream) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	resp, send, closeRun, err := c.openRun(ctx, token, run)
	if err != nil {
		return err
	}
	defer closeRun()
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/connect+proto") {
		return errors.New("cursor: unexpected stream content type")
	}
	received, events := 0, 0
	emit := func(event map[string]any) error {
		// Immutable snapshots are bounded individually; cancellable backpressure
		// bounds unread snapshots, not the sum of already-consumed snapshots.
		// Independent lifetime wire and event-work limits remain in force.
		b, err := json.Marshal(event)
		if err != nil {
			return errors.New("cursor: cannot encode stream event")
		}
		events++
		if len(b) > maxSession || events > 32768 {
			return errors.New("cursor: event budget exceeded")
		}
		var snapshot map[string]any
		if err = json.Unmarshal(b, &snapshot); err != nil {
			return err
		}
		return stream.PushWithBackpressure(ctx, snapshot)
	}
	if err := emit(map[string]any{"type": "start", "partial": msg}); err != nil {
		return err
	}
	seen := map[string][]byte{}
	calls := map[string]map[string]any{}
	partials := map[string]string{}
	aliases := map[string]string{}
	blockIndex := -1
	blockKind := ""
	closeBlock := func() error {
		if blockIndex < 0 {
			return nil
		}
		p := array(msg["content"])[blockIndex].(map[string]any)
		event := map[string]any{"type": blockKind + "_end", "contentIndex": blockIndex, "partial": msg}
		if blockKind == "thinking" {
			event["content"] = p["thinking"]
		} else {
			event["content"] = p["text"]
		}
		blockIndex = -1
		return emit(event)
	}
	ended := false
	frames := 0
	for {
		flags, b, err := readFrame(resp.Body)
		if err != nil {
			return fmt.Errorf("cursor: stream truncated or unreadable: %w", err)
		}
		received += len(b) + 5
		frames++
		if frames > 65536 {
			return errors.New("cursor: stream frame work budget exceeded")
		}
		if received > maxSession {
			return errors.New("cursor: stream byte budget exceeded")
		}
		if flags == 2 {
			var terminal struct {
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if strings.TrimSpace(string(b)) == "null" {
				return errors.New("cursor: missing terminal status")
			}
			if err = json.Unmarshal(b, &terminal); err != nil {
				return errors.New("cursor: malformed terminal status")
			}
			if terminal.Error != nil {
				code := terminal.Error.Code
				switch code {
				case "unauthenticated", "permission_denied", "resource_exhausted", "invalid_argument", "unavailable", "deadline_exceeded", "not_found":
					return fmt.Errorf("cursor: remote Connect %s (no retry)", code)
				default:
					return errors.New("cursor: remote Connect failure (no retry)")
				}
			}
			// Read through EOF so HTTP trailers and late transport errors are observed.
			var tail [1]byte
			if n, endErr := io.ReadFull(resp.Body, tail[:]); n != 0 || endErr != io.EOF {
				return errors.New("cursor: trailing data or incomplete HTTP terminal status")
			}
			if status := resp.Trailer.Get("Grpc-Status"); status != "" && status != "0" {
				return errors.New("cursor: remote gRPC failure")
			}
			if !ended {
				return errors.New("cursor: terminal status without turn end")
			}
			if err = resp.Body.Close(); err != nil {
				return &safeError{err}
			}
			if err = closeBlock(); err != nil {
				return err
			}
			if len(calls) > 0 {
				msg["stopReason"] = "toolUse"
			}
			return nil
		}
		server, err := parse(b)
		if err != nil {
			return err
		}
		switch {
		case server.has(1):
			update, err := parse(server.data(1))
			if err != nil {
				return err
			}
			if ended && !update.has(13) && !update.has(17) {
				return errors.New("cursor: interaction after turn end")
			}
			switch {
			case update.has(1) || update.has(4):
				n, kind, key := 1, "text", "text"
				if update.has(4) {
					n, kind, key = 4, "thinking", "thinking"
				}
				delta, err := parse(update.data(n))
				if err != nil {
					return err
				}
				s := delta.text(1)
				if blockIndex < 0 || blockKind != kind {
					if err = closeBlock(); err != nil {
						return err
					}
					blockKind = kind
					blockIndex = len(array(msg["content"]))
					msg["content"] = append(array(msg["content"]), map[string]any{"type": kind, key: ""})
					if err = emit(map[string]any{"type": kind + "_start", "contentIndex": blockIndex, "partial": msg}); err != nil {
						return err
					}
				}
				part := array(msg["content"])[blockIndex].(map[string]any)
				part[key] = text(part[key]) + s
				if err = emit(map[string]any{"type": kind + "_delta", "contentIndex": blockIndex, "delta": s, "partial": msg}); err != nil {
					return err
				}
			case update.has(2) || update.has(7):
				fieldNumber := 7
				if update.has(2) {
					fieldNumber = 2
				}
				partial, err := parse(update.data(fieldNumber))
				if err != nil {
					return err
				}
				envelope := partial.text(1)
				id := envelope
				if partial.has(2) {
					tool, err := parse(partial.data(2))
					if err != nil {
						return err
					}
					if tool.has(15) {
						call, err := parse(tool.data(15))
						if err != nil {
							return err
						}
						args, err := parse(call.data(1))
						if err != nil {
							return err
						}
						if actual := args.text(3); actual != "" {
							aliases[envelope] = actual
						}
					}
				}
				if actual := aliases[envelope]; actual != "" {
					id = actual
				}
				if len(aliases) > 1024 {
					return errors.New("cursor: excessive tool correlation IDs")
				}
				snapshot := ""
				if fieldNumber == 7 {
					snapshot = partial.text(3)
				}
				if strings.HasPrefix(snapshot, partials[id]) {
					partials[id] = snapshot
				} else {
					partials[id] += snapshot
				}
				if len(partials) > 1024 || len(partials[id]) > maxFrame {
					return errors.New("cursor: excessive partial tool arguments")
				}
			case update.has(14):
				ended = true
				u, err := parse(update.data(14))
				if err != nil {
					return err
				}
				usage := map[string]any{}
				for n, key := range map[int]string{1: "input", 2: "output", 3: "cacheRead", 4: "cacheWrite", 5: "reasoning"} {
					if u.has(n) {
						usage[key] = u.num(n)
					}
				}
				if len(usage) > 0 {
					msg["usage"] = usage
				}
			}
		case server.has(4):
			kv, err := parse(server.data(4))
			if err != nil {
				return err
			}
			id := integer(1, kv.num(1))
			var reply []byte
			if kv.has(2) {
				get, err := parse(kv.data(2))
				if err != nil {
					return err
				}
				var result []byte
				if data, ok := h.blobs[string(get.data(1))]; ok {
					result = bytesField(1, data)
				}
				reply = join(id, bytesField(2, result))
			} else if kv.has(3) {
				set, err := parse(kv.data(3))
				if err != nil {
					return err
				}
				key := string(set.data(1))
				if len(key) != 32 {
					return errors.New("cursor: invalid blob id")
				}
				old := len(h.blobs[key])
				h.size += len(set.data(2)) - old
				if h.size > maxSession {
					return errors.New("cursor: blob store budget exceeded")
				}
				h.blobs[key] = append([]byte(nil), set.data(2)...)
				reply = join(id, bytesField(3, nil))
			} else {
				return errors.New("cursor: unknown KV operation")
			}
			if err = send(bytesField(3, reply)); err != nil {
				return err
			}
		case server.has(2):
			exec, err := parse(server.data(2))
			if err != nil {
				return err
			}
			prefix := join(integer(1, exec.num(1)), str(15, exec.text(15)))
			if exec.has(10) {
				result := bytesField(1, bytesField(1, h.requestContext))
				if err = send(bytesField(2, join(prefix, bytesField(10, result)))); err != nil {
					return err
				}
				continue
			}
			if !exec.has(11) {
				return errors.New("cursor: unsupported remote exec operation refused; use advertised PiG MCP tools")
			}
			if ended {
				return errors.New("cursor: tool request after turn end")
			}
			args, err := parse(exec.data(11))
			if err != nil {
				return err
			}
			id, name := args.text(3), args.text(5)
			if h.completed[id] {
				return errors.New("cursor: refusing to reissue a tool call whose result is already in history")
			}
			if name == "" {
				name = args.text(1)
			}
			if id == "" || !h.tools[name] {
				return errors.New("cursor: unknown or unnamed tool request refused")
			}
			if previous, ok := seen[id]; ok {
				if string(previous) != string(exec.data(11)) {
					return errors.New("cursor: conflicting duplicate tool call")
				}
			} else {
				if len(calls) >= 128 {
					return errors.New("cursor: too many tool calls")
				}
				values := map[string]any{}
				if partials[id] != "" {
					if err = json.Unmarshal([]byte(partials[id]), &values); err != nil || values == nil {
						return errors.New("cursor: malformed streamed tool arguments")
					}
				}
				for _, entry := range args {
					if entry.number != 2 {
						continue
					}
					pair, err := parse(entry.data)
					if err != nil {
						return err
					}
					var v any
					if err = json.Unmarshal(pair.data(2), &v); err != nil {
						v = string(pair.data(2))
					}
					_, object := values[pair.text(1)].(map[string]any)
					_, list := values[pair.text(1)].([]any)
					_, fallbackString := v.(string)
					if !fallbackString || (!object && !list) {
						values[pair.text(1)] = v
					}
				}
				call := map[string]any{"type": "toolCall", "id": id, "name": name, "arguments": values}
				calls[id] = call
				seen[id] = append([]byte(nil), exec.data(11)...)
				if err = closeBlock(); err != nil {
					return err
				}
				idx := len(array(msg["content"]))
				msg["content"] = append(array(msg["content"]), call)
				if err = emit(map[string]any{"type": "toolcall_start", "contentIndex": idx, "partial": msg}); err != nil {
					return err
				}
				if err = emit(map[string]any{"type": "toolcall_end", "contentIndex": idx, "toolCall": call, "partial": msg}); err != nil {
					return err
				}
			}
			result := bytesField(1, bytesField(1, bytesField(1, str(1, handoff))))
			if err = send(bytesField(2, join(prefix, bytesField(11, result)))); err != nil {
				return err
			}
		case server.has(7), server.has(5):
			return errors.New("cursor: unsupported interaction/control request refused")
		case server.has(3):
			if _, err = parse(server.data(3)); err != nil {
				return err
			} // No cross-request checkpoint cache; rebuild from native history.
		default:
			return errors.New("cursor: unknown agent message")
		}
	}
}
