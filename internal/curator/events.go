package curator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// EventSource names the host that produced an event.
type EventSource struct {
	Host    string `json:"host"`
	EntryID string `json:"entry_id,omitempty"`
}

// EventIn is the event shape accepted by `curator ingest`.
type EventIn struct {
	ID        string       `json:"id"`
	SessionID string       `json:"session_id"`
	Category  string       `json:"category"`
	Role      string       `json:"role,omitempty"`
	ToolName  string       `json:"tool_name,omitempty"`
	CallID    string       `json:"call_id,omitempty"`
	Content   *string      `json:"content,omitempty"`
	IsError   *bool        `json:"is_error,omitempty"`
	Paths     []string     `json:"paths,omitempty"`
	Source    *EventSource `json:"source,omitempty"`
}

// Event categories.
const (
	CategoryUser      = "user_message"
	CategoryAssistant = "assistant_message"
	CategoryToolCall  = "tool_call"
	CategoryToolReply = "tool_result"
	CategoryTask      = "task_boundary"
)

const maxIDLength = 128

var pathKeys = map[string]bool{"path": true, "file": true, "file_path": true, "filepath": true, "paths": true, "files": true}

func digest16(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

// SafeID maps a host id onto curator's id alphabet `[A-Za-z0-9._:@/-]`. An id that cannot be cleaned into
// something that starts with a letter or digit and fits 128 characters becomes `h-` plus a hash of the original.
// Characters outside the BMP count as two, as UTF-16 does in the plugin this follows, so ids stay identical.
func SafeID(raw string) string {
	var cleaned strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', strings.ContainsRune("._:@/-", r):
			cleaned.WriteRune(r)
		case r > 0xFFFF:
			cleaned.WriteString("--")
		default:
			cleaned.WriteByte('-')
		}
	}
	id := cleaned.String()
	if id != "" && isAlnum(id[0]) && len(id) <= maxIDLength {
		return id
	}
	return "h-" + digest16(raw)
}

func isAlnum(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

// visibleText returns the visible text of a message body: a string, or the text blocks joined by newlines.
// Thinking and every other block type are never captured.
func visibleText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok || block["type"] != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func pathsOf(args any) []string {
	object, ok := args.(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	// The host hands arguments over as a Go map, so insertion order is gone; a fixed order keeps paths stable.
	for _, key := range slices.Sorted(maps.Keys(object)) {
		if !pathKeys[key] {
			continue
		}
		switch value := object[key].(type) {
		case string:
			out = append(out, value)
		case []any:
			for _, item := range value {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
		}
	}
	return out
}

// timestampText formats a numeric message timestamp the way JavaScript's String(number) does for ordinary values.
func timestampText(value any) (string, bool) {
	switch n := value.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), true
	case json.Number:
		return n.String(), true
	case int:
		return strconv.Itoa(n), true
	case int64:
		return strconv.FormatInt(n, 10), true
	}
	return "", false
}

func stamp(message map[string]any, role, text string) string {
	if at, ok := timestampText(message["timestamp"]); ok {
		return role + ":" + at
	}
	return role + ":" + digest16(text)
}

// CallPaths remembers the paths a tool call named so its result can carry them. It is safe for concurrent use and
// forgets the oldest call beyond limit.
type CallPaths struct {
	mu    sync.Mutex
	limit int
	order []string
	paths map[string][]string
}

// NewCallPaths returns a map that holds at most limit calls (256 when limit <= 0).
func NewCallPaths(limit int) *CallPaths {
	if limit <= 0 {
		limit = 256
	}
	return &CallPaths{limit: limit, paths: map[string][]string{}}
}

// Set records the paths of a call; a call without paths is not stored.
func (c *CallPaths) Set(callID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.paths[callID]; !exists {
		c.order = append(c.order, callID)
	}
	c.paths[callID] = paths
	if len(c.paths) > c.limit {
		delete(c.paths, c.order[0])
		c.order = c.order[1:]
	}
}

// Take returns and forgets the paths of a call.
func (c *CallPaths) Take(callID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	paths, exists := c.paths[callID]
	if !exists {
		return nil
	}
	delete(c.paths, callID)
	if i := slices.Index(c.order, callID); i >= 0 {
		c.order = slices.Delete(c.order, i, i+1)
	}
	return paths
}

func compactJSON(value any) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprintf("%v", value)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func hostSource() *EventSource { return &EventSource{Host: "omp"} }

// EventsFromMessage converts one finished host message into curator events. Roles that are not part of the visible
// transcript (including the extension's own injected history) are ignored, and private reasoning is never read.
func EventsFromMessage(sessionID string, message any, calls *CallPaths) []EventIn {
	msg, ok := message.(map[string]any)
	if !ok {
		return nil
	}
	role, ok := msg["role"].(string)
	if !ok {
		return nil
	}
	switch role {
	case "user":
		content := visibleText(msg["content"])
		if content == "" {
			return nil
		}
		return []EventIn{{ID: SafeID(stamp(msg, "user", content)), SessionID: sessionID, Category: CategoryUser, Role: "user", Content: &content, Source: hostSource()}}
	case "assistant":
		var events []EventIn
		if content := visibleText(msg["content"]); content != "" {
			events = append(events, EventIn{ID: SafeID(stamp(msg, "assistant", content)), SessionID: sessionID, Category: CategoryAssistant, Role: "assistant", Content: &content, Source: hostSource()})
		}
		blocks, _ := msg["content"].([]any)
		for _, item := range blocks {
			block, ok := item.(map[string]any)
			if !ok || block["type"] != "toolCall" {
				continue
			}
			id, idOK := block["id"].(string)
			name, nameOK := block["name"].(string)
			if !idOK || !nameOK {
				continue
			}
			paths := pathsOf(block["arguments"])
			calls.Set(id, paths)
			arguments := block["arguments"]
			if arguments == nil {
				arguments = map[string]any{}
			}
			content := compactJSON(arguments)
			events = append(events, EventIn{ID: SafeID("call:" + id), SessionID: sessionID, Category: CategoryToolCall, ToolName: name, CallID: id, Content: &content, Paths: paths, Source: hostSource()})
		}
		return events
	case "toolResult":
		callID, ok := msg["toolCallId"].(string)
		if !ok {
			return nil
		}
		paths := calls.Take(callID)
		name, _ := msg["toolName"].(string)
		// File contents stay on disk, and the matching tool_call already records the path.
		if name == "read" {
			return nil
		}
		content := visibleText(msg["content"])
		event := EventIn{ID: SafeID("result:" + callID), SessionID: sessionID, Category: CategoryToolReply, ToolName: name, CallID: callID, Content: &content, Paths: paths, Source: hostSource()}
		if isError, ok := msg["isError"].(bool); ok {
			event.IsError = &isError
		}
		return []EventIn{event}
	}
	return nil
}

// TaskBoundary closes the episode when the agent loop ends.
func TaskBoundary(sessionID string, messages any) EventIn {
	last := "end"
	if list, ok := messages.([]any); ok && len(list) > 0 {
		if tail, ok := list[len(list)-1].(map[string]any); ok {
			if at, ok := timestampText(tail["timestamp"]); ok {
				last = at
			}
		}
	}
	return EventIn{ID: SafeID("task:" + last), SessionID: sessionID, Category: CategoryTask, Source: hostSource()}
}
