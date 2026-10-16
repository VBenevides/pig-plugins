package cursordirect

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
)

type history struct {
	blobs          map[string][]byte
	size           int
	tools          map[string]bool
	requestContext []byte
	completed      map[string]bool
}

func (h *history) store(b []byte) []byte {
	id := sha256.Sum256(b)
	if _, ok := h.blobs[string(id[:])]; !ok {
		h.blobs[string(id[:])] = b
		h.size += len(b)
	}
	return id[:]
}
func text(v any) string { s, _ := v.(string); return s }
func objects(v any) []map[string]any {
	var out []map[string]any
	for _, v := range array(v) {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func array(v any) []any { a, _ := v.([]any); return a }

func uuidID(seed string) string {
	b := sha256.Sum256([]byte(seed))
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func wireCallID(id string) string {
	for _, r := range id {
		if r != '_' && r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return fmt.Sprintf("pig_%x", sha256.Sum256([]byte(id)))
		}
	}
	return id
}

// Normalize the SDK's leading system/toolsAdded/toolsRemoved representation,
// retaining later system-message ordering and the current effective tool set.
func normalizeNativeTranscript(request map[string]any) error {
	messages, ok := request["messages"].([]any)
	if !ok {
		return errors.New("cursor: transcript messages must be an array")
	}
	tools := map[string]map[string]any{}
	order := []string{}
	for _, tool := range objects(request["tools"]) {
		name := text(tool["name"])
		if name == "" || tools[name] != nil {
			return errors.New("cursor: missing or duplicate tool name")
		}
		tools[name] = tool
		order = append(order, name)
	}
	leading := true
	normalized := []any{}
	prompts := []any{}
	switch p := request["systemPrompt"].(type) {
	case string:
		prompts = append(prompts, p)
	case []any:
		prompts = append(prompts, p...)
	case nil:
	default:
		return errors.New("cursor: invalid system prompt")
	}
	for _, raw := range messages {
		m, ok := raw.(map[string]any)
		if !ok {
			return errors.New("cursor: invalid transcript message")
		}
		if m["role"] != "system" {
			leading = false
			normalized = append(normalized, m)
			continue
		}
		for _, removal := range objects(m["toolsRemoved"]) {
			delete(tools, text(removal["name"]))
		}
		for _, tool := range objects(m["toolsAdded"]) {
			name := text(tool["name"])
			if name == "" {
				return errors.New("cursor: unnamed added tool")
			}
			if tools[name] == nil {
				order = append(order, name)
			}
			tools[name] = tool
		}
		if leading {
			s, err := contentText(m["content"])
			if err != nil {
				return err
			}
			if s != "" {
				prompts = append(prompts, s)
			}
		} else {
			normalized = append(normalized, m)
		}
	}
	effective := []any{}
	emitted := map[string]bool{}
	for _, name := range order {
		if t := tools[name]; t != nil && !emitted[name] {
			effective = append(effective, t)
			emitted[name] = true
		}
	}
	request["systemPrompt"] = prompts
	request["tools"] = effective
	request["messages"] = normalized
	return nil
}
func contentText(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	var out string
	for _, p := range objects(v) {
		switch text(p["type"]) {
		case "text":
			out += text(p["text"])
		case "thinking", "toolCall":
		default:
			return "", errors.New("cursor: unsupported content (text and native tools only)")
		}
	}
	return out, nil
}
func marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, errors.New("cursor: cannot encode request JSON")
	}
	if len(b) > maxFrame {
		return nil, errors.New("cursor: request exceeds size limit")
	}
	return b, nil
}
func mcpArgs(id, name string, args map[string]any) ([]byte, error) {
	b := join(str(1, name), str(3, id), str(4, "pi-agent"), str(5, name))
	for k, v := range args {
		j, err := marshal(v)
		if err != nil {
			return nil, err
		}
		b = append(b, bytesField(2, join(str(1, k), bytesField(2, j)))...)
	}
	return b, nil
}
func mcpResult(result map[string]any) ([]byte, error) {
	s, err := contentText(result["content"])
	if err != nil {
		return nil, err
	}
	if result["isError"] == true {
		return bytesField(2, str(1, s)), nil
	}
	return bytesField(1, bytesField(1, bytesField(1, str(1, s)))), nil
}
func buildHistory(model, request map[string]any) (*history, []byte, error) {
	// Normalize caller slices/maps and reject non-JSON data before any network I/O.
	b, err := marshal(request)
	if err != nil {
		return nil, nil, err
	}
	var normalized map[string]any
	if err = json.Unmarshal(b, &normalized); err != nil {
		return nil, nil, err
	}
	request = normalized
	if err := normalizeNativeTranscript(normalized); err != nil {
		return nil, nil, err
	}
	h := &history{blobs: map[string][]byte{}, tools: map[string]bool{}}
	var state []byte
	systems := []string{}
	switch v := request["systemPrompt"].(type) {
	case string:
		systems = append(systems, v)
	case []any:
		for _, p := range v {
			if s, ok := p.(string); ok {
				systems = append(systems, s)
			} else {
				return nil, nil, errors.New("cursor: invalid system prompt")
			}
		}
	}
	if len(systems) == 0 {
		systems = []string{"You are a helpful assistant."}
	}
	for i, s := range systems {
		j, err := marshal(map[string]any{"role": "system", "content": s})
		if err != nil {
			return nil, nil, err
		}
		state = append(state, bytesField(1, h.store(j))...)
		rule := join(str(1, fmt.Sprintf("/pig/system-prompt/%d.mdc", i)), str(2, s), bytesField(3, bytesField(1, nil)), integer(4, 2))
		h.requestContext = append(h.requestContext, bytesField(2, rule)...)
	}
	// Cursor's built-in exec tools cannot bypass PiG approvals. Expose only the
	// native MCP proposals that the external-client handoff can safely return.
	safetyRule := join(str(1, "/pig/tool-boundary.mdc"), str(2, "Use only the advertised MCP tools for local actions. Do not use built-in shell, filesystem, edit, web or other exec tools. The external PiG client executes MCP proposals after approval and supplies results in the next request."), bytesField(3, bytesField(1, nil)), integer(4, 2))
	h.requestContext = append(h.requestContext, bytesField(2, safetyRule)...)
	for _, t := range objects(request["tools"]) {
		name := text(t["name"])
		if name == "" || h.tools[name] {
			return nil, nil, errors.New("cursor: missing or duplicate tool name")
		}
		h.tools[name] = true
		schema := t["parameters"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		j, err := marshal(schema)
		if err != nil {
			return nil, nil, err
		}
		// Advertise every local operation as MCP; never allow remote native execution.
		def := join(str(1, name), str(2, text(t["description"])), bytesField(3, j), str(4, "pi-agent"), str(5, name), str(6, string(j)))
		h.requestContext = append(h.requestContext, bytesField(7, def)...)
	}
	msgs := objects(request["messages"])
	if len(msgs) == 0 {
		return nil, nil, errors.New("cursor: empty transcript")
	}
	active := -1
	if r := text(msgs[len(msgs)-1]["role"]); r == "user" || r == "developer" {
		active = len(msgs) - 1
	}
	results := map[string]map[string]any{}
	h.completed = map[string]bool{}
	for _, m := range msgs {
		if m["role"] == "toolResult" {
			results[text(m["toolCallId"])] = m
			h.completed[text(m["toolCallId"])] = true
			h.completed[wireCallID(text(m["toolCallId"]))] = true
		}
	}
	var turn []byte
	flush := func() {
		if turn != nil {
			state = append(state, bytesField(8, h.store(bytesField(1, turn)))...)
			turn = nil
		}
	}
	issued := map[string]bool{}
	for i, m := range msgs {
		s, err := contentText(m["content"])
		if err != nil {
			return nil, nil, err
		}
		role := text(m["role"])
		if i == active {
			break
		}
		var root map[string]any
		switch role {
		case "system":
			root = map[string]any{"role": "system", "content": s}
		case "user", "developer":
			flush()
			turn = bytesField(1, h.store(join(str(1, s), str(2, uuidID(fmt.Sprintf("history-%d:%s", i, s))), integer(4, 1))))
			root = map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": s}}}
		case "assistant":
			parts := []any{}
			for _, p := range objects(m["content"]) {
				switch p["type"] {
				case "text":
					parts = append(parts, map[string]any{"type": "text", "text": p["text"]})
					if turn != nil {
						step := bytesField(1, str(1, text(p["text"])))
						turn = append(turn, bytesField(2, h.store(step))...)
					}
				case "thinking": // Unsigned thinking is not replayable across models.
				case "toolCall":
					id, name := text(p["id"]), text(p["name"])
					if id == "" || name == "" {
						return nil, nil, errors.New("cursor: invalid history tool call")
					}
					if issued[id] {
						continue
					}
					issued[id] = true
					args, ok := p["arguments"].(map[string]any)
					if !ok {
						return nil, nil, errors.New("cursor: tool arguments must be an object")
					}
					wireID := wireCallID(id)
					parts = append(parts, map[string]any{"type": "tool-call", "toolCallId": wireID, "toolName": name, "args": args})
					a, err := mcpArgs(wireID, name, args)
					if err != nil {
						return nil, nil, err
					}
					call := bytesField(1, a)
					if r := results[id]; r != nil {
						result, err := mcpResult(r)
						if err != nil {
							return nil, nil, err
						}
						call = append(call, bytesField(2, result)...)
					}
					if turn != nil {
						step := bytesField(2, join(bytesField(15, call), str(57, wireID)))
						turn = append(turn, bytesField(2, h.store(step))...)
					}
				}
			}
			root = map[string]any{"role": "assistant", "content": parts}
		case "toolResult":
			id := text(m["toolCallId"])
			if !issued[id] {
				return nil, nil, errors.New("cursor: orphan tool result in transcript")
			}
			wireID := wireCallID(id)
			root = map[string]any{"role": "tool", "id": wireID, "content": []any{map[string]any{"type": "tool-result", "toolName": m["toolName"], "toolCallId": wireID, "result": s, "isError": m["isError"] == true}}}
		default:
			return nil, nil, fmt.Errorf("cursor: unsupported transcript role %q", role)
		}
		j, err := marshal(root)
		if err != nil {
			return nil, nil, err
		}
		state = append(state, bytesField(1, h.store(j))...)
	}
	flush()
	action := bytesField(2, nil)
	if active >= 0 {
		s, err := contentText(msgs[active]["content"])
		if err != nil {
			return nil, nil, err
		}
		if s == "" {
			return nil, nil, errors.New("cursor: empty user action")
		}
		user := join(str(1, s), str(2, uuidID(rand.Text())), integer(4, 1))
		action = bytesField(1, bytesField(1, user))
	}
	id := text(model["id"])
	if id == "" {
		return nil, nil, errors.New("cursor: model id is missing")
	}
	maxMode := uint64(0)
	if model["cursorMaxMode"] == true {
		maxMode = 1
	}
	details := join(str(1, id), str(3, id), str(4, text(model["name"])), integer(7, maxMode))
	run := join(bytesField(1, state), bytesField(2, action), bytesField(3, details), str(5, uuidID(rand.Text())), bytesField(9, join(str(1, id), integer(2, maxMode))))
	if h.size > maxSession || len(run) > maxFrame {
		return nil, nil, errors.New("cursor: conversation exceeds size limit")
	}
	return h, bytesField(1, run), nil
}
