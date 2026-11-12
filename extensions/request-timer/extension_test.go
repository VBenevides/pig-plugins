package requesttimerext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestTimerSpansToolFollowUpAndClearsAtEnd(t *testing.T) {
	fused := pigtest.UseFusedBinary(t)
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": "missing-file"})}, Delay: 1200 * time.Millisecond},
		pigtest.Reply{Text: "done", Delay: 1200 * time.Millisecond},
		pigtest.Reply{Text: "next", Delay: 1200 * time.Millisecond},
	)
	defer mock.Close()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	extensions := []string{path}
	if fused {
		extensions = nil
	}
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensions, Prompts: []string{"test timer", "next request"}})
	if len(mock.Requests()) != 3 {
		t.Fatalf("completion record started an extra turn: %d requests", len(mock.Requests()))
	}
	files, err := filepath.Glob(filepath.Join(home.AgentDir(), "sessions", "*", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var records []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			var entry struct {
				Type       string          `json:"type"`
				CustomType string          `json:"customType"`
				Content    json.RawMessage `json:"content"`
				Display    bool            `json:"display"`
			}
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatal(err)
			}
			if entry.Type != "custom_message" || entry.CustomType != Name {
				continue
			}
			if !entry.Display {
				t.Fatal("elapsed record is hidden")
			}
			var content string
			if err := json.Unmarshal(entry.Content, &content); err != nil {
				t.Fatal(err)
			}
			records = append(records, content)
		}
	}
	if len(records) != 2 {
		t.Fatalf("durable elapsed records = %v", records)
	}
	for i, minimum := range []time.Duration{2 * time.Second, time.Second} {
		if !strings.HasPrefix(records[i], "Elapsed - ") {
			t.Fatal(records)
		}
		elapsed, err := time.ParseDuration(strings.ReplaceAll(strings.TrimPrefix(records[i], "Elapsed - "), " ", ""))
		if err != nil || elapsed < minimum {
			t.Fatalf("elapsed record %d = %q (%v); want at least %s", i, records[i], err, minimum)
		}
	}
}
