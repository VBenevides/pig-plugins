package cursordirect

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestNativeTranscriptSystemAndToolChanges(t *testing.T) {
	request := map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": []any{map[string]any{"type": "text", "text": "native system"}}, "toolsAdded": []any{map[string]any{"name": "old"}}},
		map[string]any{"role": "user", "content": "first"},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "answer"}}},
		map[string]any{"role": "system", "content": "later instruction", "toolsRemoved": []any{map[string]any{"name": "old"}}, "toolsAdded": []any{map[string]any{"name": "new", "parameters": map[string]any{"type": "object"}}}},
		map[string]any{"role": "user", "content": "second"},
	}}
	h, run, err := buildHistory(model(), request)
	if err != nil {
		t.Fatal(err)
	}
	if h.tools["old"] || !h.tools["new"] {
		t.Fatal(h.tools)
	}
	state := mustParse(t, mustParse(t, run).data(1))
	state = mustParse(t, state.data(1))
	var roles []string
	var contents []string
	for _, f := range state {
		if f.number != 1 {
			continue
		}
		var root map[string]any
		if err = json.Unmarshal(h.blobs[string(f.data)], &root); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, text(root["role"]))
		s, _ := contentText(root["content"])
		contents = append(contents, s)
	}
	if len(roles) != 4 || roles[0] != "system" || contents[0] != "native system" || roles[3] != "system" || contents[3] != "later instruction" {
		t.Fatalf("roles=%v contents=%v", roles, contents)
	}
}
func TestForeignToolIDsStayPaired(t *testing.T) {
	r := toolsPrompt()
	r["messages"] = append(array(r["messages"]), map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "call|foreign", "name": "read", "arguments": map[string]any{}}}}, map[string]any{"role": "toolResult", "toolCallId": "call|foreign", "toolName": "read", "content": "result"})
	h, _, err := buildHistory(model(), r)
	if err != nil {
		t.Fatal(err)
	}
	var call, result string
	for _, b := range h.blobs {
		var root map[string]any
		if json.Unmarshal(b, &root) != nil {
			continue
		}
		for _, part := range objects(root["content"]) {
			switch part["type"] {
			case "tool-call":
				call = text(part["toolCallId"])
			case "tool-result":
				result = text(part["toolCallId"])
			}
		}
	}
	if call == "" || call != result || call == "call|foreign" {
		t.Fatalf("call=%s result=%s", call, result)
	}
}
func TestInterleavedToolSnapshotsAndNullRejection(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "structured snapshots", true: "null rejected"}[invalid], func(t *testing.T) {
			c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _, _ = readFrame(r.Body)
				writeFrames(w)
				args, _ := mcpArgs("tool-id", "read", map[string]any{"nested": "downgraded"})
				started := join(str(1, "envelope-id"), bytesField(2, bytesField(15, bytesField(1, args))), str(3, "model-call-id"))
				_, _ = w.Write(frame(update(2, started)))
				snapshot := `{"nested":{"value":42}}`
				if invalid {
					snapshot = "null"
				}
				for _, s := range []string{snapshot[:2], snapshot} {
					_, _ = w.Write(frame(update(7, join(str(1, "envelope-id"), str(3, s)))))
				}
				_, _ = w.Write(frame(bytesField(2, join(integer(1, 2), bytesField(11, args)))))
				w.(http.Flusher).Flush()
				if invalid {
					return
				}
				_, _, err := readFrame(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = w.Write(frame(update(14, nil)))
				_, _ = w.Write(terminal("{}"))
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			m, _ := result(t, c, ctx, toolsPrompt())
			if invalid {
				if m["stopReason"] != "error" {
					t.Fatal(m)
				}
				return
			}
			if m["stopReason"] != "toolUse" {
				t.Fatal(m)
			}
			args := objects(m["content"])[0]["arguments"].(map[string]any)
			if args["nested"].(map[string]any)["value"] != float64(42) {
				t.Fatal(args)
			}
		})
	}
}

func TestCompletedToolCannotBeReissued(t *testing.T) {
	r := toolsPrompt()
	r["messages"] = append(array(r["messages"]), map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "completed", "name": "read", "arguments": map[string]any{}}}}, map[string]any{"role": "toolResult", "toolCallId": "completed", "toolName": "read", "content": "already durable"})
	c := fakeClient(t, func(w http.ResponseWriter, req *http.Request) {
		_, _, _ = readFrame(req.Body)
		args, _ := mcpArgs("completed", "read", map[string]any{})
		writeFrames(w, frame(bytesField(2, join(integer(1, 1), bytesField(11, args)))))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m, events := result(t, c, ctx, r)
	if m["stopReason"] != "error" {
		t.Fatal(m)
	}
	for _, event := range events {
		if event["type"] == "toolcall_end" {
			t.Fatal("completed action was proposed again")
		}
	}
}
