package imageviewext

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// Exercises the real SDK wire, retained session context, draft polling and input
// handler together; the host fixture models atomic owner-loop replacement.
func TestDraftPreviewAndSubmissionThroughSDK(t *testing.T) {
	for _, graphics := range []bool{false, true} {
		name := "fallback"
		if graphics {
			name = "graphics"
		}
		for _, observedClear := range []bool{false, true} {
			timing := "submit-first"
			if observedClear {
				timing = "poll-clear-first"
			}
			t.Run(name+"/"+timing, func(t *testing.T) { testDraftPreviewAndSubmission(t, graphics, observedClear) })
		}
	}
}

func testDraftPreviewAndSubmission(t *testing.T, graphics, observedClear bool) {
	t.Setenv("PIG_CODING_AGENT_DIR", t.TempDir())
	path := draftPNG(t, "clipboard.png")
	host, client := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- Extension().RunWithConn(client) }()
	t.Cleanup(func() {
		_ = host.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SDK did not stop")
		}
	})
	read := func() map[string]any {
		t.Helper()
		if err := host.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var header [4]byte
		if _, err := io.ReadFull(host, header[:]); err != nil {
			t.Fatal(err)
		}
		length := binary.BigEndian.Uint32(header[:])
		if length > 8<<20 {
			t.Fatal("oversized SDK frame")
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(host, data); err != nil {
			t.Fatal(err)
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatal(err)
		}
		return frame
	}
	write := func(frame map[string]any) {
		t.Helper()
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(len(data)))
		if err := host.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := host.Write(append(header[:], data...)); err != nil {
			t.Fatal(err)
		}
	}
	registered := read()
	if registered["type"] != "register" {
		t.Fatalf("registration=%v", registered)
	}
	handlers := make(map[string]any)
	for _, value := range registered["register"].(map[string]any)["handlers"].([]any) {
		handler := value.(map[string]any)
		handlers[handler["event"].(string)] = handler["handler_id"]
	}
	write(map[string]any{"type": "ready", "ready": map[string]any{"mode": "tui", "cwd": t.TempDir(), "width": 80, "state": map[string]any{"hasUI": true}}})
	editorText := ""
	previewLabel := ""
	previewWidth, previewRows := 0, 0
	previewUpdates := 0
	serve := func(frame map[string]any) {
		t.Helper()
		if frame["type"] != "call" {
			return
		}
		call := frame["call"].(map[string]any)
		args, _ := call["args"].(map[string]any)
		var result any
		switch call["method"] {
		case "sessionRead":
			if args["method"] != "getBranch" {
				t.Fatalf("unexpected session read: %v", args)
			}
			result = []any{}
		case "ui.getEditorText":
			result = map[string]any{"text": editorText}
		case "ui.compareAndSetEditorText":
			applied := editorText == args["expected"]
			if applied {
				editorText = args["text"].(string)
			}
			result = map[string]any{"applied": applied}
		case "ui.setWidget":
			if args["key"] == "image-view" && args["content"] == nil {
				previewLabel, previewRows = "", 0
			}
		case "ui.renderImage":
			lines := []string{}
			if graphics {
				lines = []string{"\x1b_Gpreview\x1b\\"}
			}
			result = map[string]any{"frame": map[string]any{"supported": graphics, "lines": lines}}
		case "ui.setPreviewWidget":
			previewUpdates++
			lines := args["lines"].([]any)
			previewLabel = ""
			for _, line := range lines {
				if text := line.(string); strings.HasPrefix(text, "[Image #") {
					previewLabel += text
				}
			}
			previewWidth, previewRows = int(args["width"].(float64)), len(lines)
			imageRow := "▀"
			if graphics {
				imageRow = "\x1b_G"
			}
			if len(lines) < 2 || !strings.Contains(lines[1].(string), imageRow) {
				t.Fatal("gallery is missing image rows")
			}
		case "ui.notify":
			t.Fatalf("unexpected warning: %v", args)
		default:
			t.Fatalf("unexpected host call: %v", call)
		}
		write(map[string]any{"type": "call_result", "id": frame["id"], "call_result": map[string]any{"result": result}})
	}
	until := func(match func(map[string]any) bool) map[string]any {
		t.Helper()
		for {
			frame := read()
			serve(frame)
			if match(frame) {
				return frame
			}
		}
	}
	sendEvent := func(id, event string, args map[string]any) {
		write(map[string]any{"type": "request", "id": id, "request": map[string]any{"method": "event", "event": event, "handler_id": handlers[event], "args": args}})
	}
	response := func(id string) map[string]any {
		frame := until(func(frame map[string]any) bool { return frame["type"] == "response" && frame["id"] == id })
		value := frame["response"].(map[string]any)
		if value["error"] != nil {
			t.Fatalf("handler failed: %v", value)
		}
		return value
	}
	sendEvent("start", "session_start", map[string]any{})
	response("start")
	// No submission event is sent until both the draft marker and preview arrive.
	editorText = "inspect " + path
	until(func(_ map[string]any) bool { return previewLabel == "[Image #1]" })
	if editorText != "inspect [Image #1]" {
		t.Fatalf("draft=%q", editorText)
	}
	editorText += " and " + path
	until(func(_ map[string]any) bool {
		return strings.Contains(previewLabel, "[Image #1]") && strings.Contains(previewLabel, "[Image #2]")
	})
	if editorText != "inspect [Image #1] and [Image #2]" {
		t.Fatalf("multi-image draft=%q", editorText)
	}
	write(map[string]any{"type": "notify", "notify": map[string]any{"method": "width_change", "args": map[string]any{"width": 40}}})
	until(func(_ map[string]any) bool { return previewWidth == 40 })
	if !strings.Contains(previewLabel, "[Image #1]") || !strings.Contains(previewLabel, "[Image #2]") || previewRows < 4 {
		t.Fatalf("resize lost gallery rows: %q (%d rows)", previewLabel, previewRows)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// Typing immediately before Enter can be newer than the poller's last text.
	text := editorText + " Explain these images"
	editorText = "" // The owner clears the draft before submitting its snapshot.
	if observedClear {
		// Force the poller to observe clearing before the delayed submission event.
		until(func(frame map[string]any) bool {
			if frame["type"] != "call" {
				return false
			}
			return frame["call"].(map[string]any)["method"] == "ui.setWidget"
		})
	}
	updatesBeforeSubmission := previewUpdates
	sendEvent("submit", "input", map[string]any{"text": text, "images": []any{}})
	result := response("submit")["result"].(map[string]any)
	if result["action"] != "transform" || len(result["images"].([]any)) != 2 {
		t.Fatalf("submission=%v", result)
	}
	if !strings.Contains(result["text"].(string), "[[Image #1]](image-view://") {
		t.Fatalf("marker renumbered or not persisted: %v", result["text"])
	}
	if previewRows != 0 || previewLabel != "" || previewUpdates != updatesBeforeSubmission {
		t.Fatalf("submitted draft gallery remains visible or was republished: %q (%d rows)", previewLabel, previewRows)
	}
	// A normal later message must not resend the consumed attachment.
	sendEvent("later", "input", map[string]any{"text": "hello", "images": []any{}})
	if result := response("later")["result"].(map[string]any); result["action"] != "continue" {
		t.Fatalf("attachment repeated: %v", result)
	}
	sendEvent("stop", "session_shutdown", map[string]any{})
	response("stop")
}
