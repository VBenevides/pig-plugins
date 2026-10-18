package commandoutput_test

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	commandoutput "github.com/VBenevides/pig-plugins/extensions/command-output"
)

func TestReducerSettingsLiveThroughSDK(t *testing.T) {
	// The old enable variable must not override the settings value.
	t.Setenv("PI_RTK", "1")
	t.Setenv("PI_RTK_BYPASS", "0")
	host, client := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- commandoutput.Extension().RunWithConn(client) }()
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
			t.Fatal("oversized frame")
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
	handlers := map[string]any{}
	for _, value := range registered["register"].(map[string]any)["handlers"].([]any) {
		handler := value.(map[string]any)
		handlers[handler["event"].(string)] = handler["handler_id"]
	}
	write(map[string]any{"type": "ready", "ready": map[string]any{"cwd": t.TempDir(), "mode": "print", "state": map[string]any{"settings": map[string]any{}}}})
	raw := strings.Repeat("=== RUN   TestMany\n--- PASS: TestMany (0.00s)\n", 100) + "PASS\nok\texample\t0.01s\n"
	event := map[string]any{"toolName": "bash", "input": map[string]any{"command": "go test -v ./..."}, "content": []any{map[string]any{"type": "text", "text": raw}}}
	request := func(id, name string, args map[string]any) any {
		t.Helper()
		write(map[string]any{"type": "request", "id": id, "request": map[string]any{"method": "event", "event": name, "handler_id": handlers[name], "args": args}})
		frame := read()
		for frame["type"] == "request_state" {
			frame = read()
		}
		if frame["type"] != "response" || frame["id"] != id {
			t.Fatalf("unexpected frame: %v", frame)
		}
		response := frame["response"].(map[string]any)
		if response["error"] != nil {
			t.Fatalf("handler failed: %v", response)
		}
		return response["result"]
	}
	for i, tc := range []struct {
		settings map[string]any
		bypass   bool
		reduced  bool
	}{
		{map[string]any{}, false, false},
		{map[string]any{"reduceCommandOutput": true}, false, true},
		{map[string]any{"reduceCommandOutput": false}, false, false},
		{map[string]any{"reduceCommandOutput": "true"}, false, false},
		{map[string]any{"reduceCommandOutput": true}, true, false},
		{map[string]any{"reduceCommandOutput": true}, false, true},
	} {
		t.Setenv("PI_RTK_BYPASS", map[bool]string{false: "0", true: "1"}[tc.bypass])
		write(map[string]any{"type": "notify", "notify": map[string]any{"method": "state_update", "args": map[string]any{"state": map[string]any{"settings": tc.settings}}}})
		result := request(fmt.Sprint(i), "tool_result", event)
		if tc.reduced {
			data, err := json.Marshal(result)
			if err != nil || !strings.Contains(string(data), "Raw capture:") || strings.Contains(string(data), "=== RUN") {
				t.Fatalf("not reduced: %s %v", data, err)
			}
		} else if result != nil {
			t.Fatalf("disabled reducer overrode output: %v", result)
		}
	}
	request("shutdown", "session_shutdown", map[string]any{})
}
