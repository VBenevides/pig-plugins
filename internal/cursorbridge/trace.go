package cursorbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// traceEnv names an opt-in JSONL file for live diagnosis. Only structural
// fields are written: event type, content-block types and tool names (capped).
// Prompts, answers, reasoning, arguments and credentials are never recorded.
const traceEnv = "PIG_CURSOR_TRACE"

func traceWrite(record map[string]any) {
	path := os.Getenv(traceEnv)
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		traceFailure(err)
		return
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		traceFailure(err)
	}
}

var traceFailed sync.Once

// traceFailure reports the first trace write error once. Tracing never changes
// the bridge request, so a failed trace must not fail the turn.
func traceFailure(err error) {
	traceFailed.Do(func() { fmt.Fprintf(os.Stderr, "cursor trace: cannot write %s: %v\n", traceEnv, err) })
}

func capName(value string) string {
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

func traceEvent(data json.RawMessage) {
	traceToolShape(data)
	if os.Getenv(traceEnv) == "" {
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil {
		traceWrite(map[string]any{"event": "undecodable"})
		return
	}
	keys := make([]string, 0, len(envelope))
	for key := range envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	record := map[string]any{"event": "stream", "keys": keys}
	for _, channel := range []string{"sdkMessage", "interactionUpdate", "step"} {
		var item struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
				Message struct {
					Content []struct {
						Type string `json:"type"`
						Name string `json:"name"`
					} `json:"content"`
				} `json:"message"`
			} `json:"message"`
		}
		if raw, ok := envelope[channel]; ok && json.Unmarshal(raw, &item) == nil {
			record["channel"], record["type"], record["name"], record["status"] = channel, capName(item.Type), capName(item.Name), capName(item.Status)
			var blocks []string
			for _, block := range item.Message.Content {
				blocks = append(blocks, capName(block.Type+":"+block.Name))
			}
			for _, block := range item.Message.Message.Content {
				blocks = append(blocks, capName(block.Type+":"+block.Name))
			}
			record["blocks"] = blocks
		}
	}
	traceWrite(record)
}

func traceCallbacks(server *ToolServer) {
	if os.Getenv(traceEnv) == "" {
		return
	}
	proposals, violation := server.Results()
	traceWrite(map[string]any{"event": "callbacks", "proposals": len(proposals), "violation": violation != nil})
}

func traceCallbackHit(stage string) {
	if os.Getenv(traceEnv) != "" {
		traceWrite(map[string]any{"event": "callback-hit", "stage": stage})
	}
}

// skeleton keeps structure and a few identifier fields. Every other string is
// replaced by its length so prompts, results and arguments are not recorded.
func skeleton(key string, value any, depth int) any {
	if depth > 8 {
		return "<deep>"
	}
	switch v := value.(type) {
	case string:
		switch key {
		case "type", "toolName", "providerIdentifier", "name", "status", "serverName", "case":
			return capName(v)
		}
		return len(v)
	case []any:
		out := make([]any, 0, len(v))
		for i, item := range v {
			if i == 12 {
				break
			}
			out = append(out, skeleton(key, item, depth+1))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			out[k] = skeleton(k, item, depth+1)
		}
		return out
	}
	return value
}

func traceToolShape(data json.RawMessage) {
	if os.Getenv(traceEnv) == "" {
		return
	}
	var envelope map[string]any
	if json.Unmarshal(data, &envelope) != nil {
		return
	}
	for _, channel := range []string{"interactionUpdate", "step"} {
		if item, ok := envelope[channel].(map[string]any); ok {
			if kind, _ := item["type"].(string); strings.Contains(strings.ToLower(kind), "tool") {
				traceWrite(map[string]any{"event": "tool-shape", "shape": skeleton("", item, 0)})
			}
		}
	}
}

// TraceRequestShape records the top-level keys and tool count of a PiG request.
func TraceRequestShape(keys []string, toolCount int) {
	if os.Getenv(traceEnv) != "" {
		sort.Strings(keys)
		traceWrite(map[string]any{"event": "request", "keys": keys, "tools": toolCount})
	}
}
