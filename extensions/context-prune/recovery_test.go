package contextprune

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestProviderRecoversSavedOutputWithoutRerunningTheRead(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	original := "RECOVERY_ORIGINAL_NEEDLE\n" + strings.Repeat("original source that must remain available\n", 2500)
	if err := os.WriteFile(filepath.Join(home.Work, "old.txt"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "tiny.txt"), []byte("tiny\n"), 0600); err != nil {
		t.Fatal(err)
	}
	replies := []pigtest.Reply{pigtest.Calls(pigtest.Call("read", map[string]any{"path": "old.txt"}))}
	for range 15 {
		replies = append(replies, pigtest.Calls(pigtest.Call("read", map[string]any{"path": "tiny.txt"})))
	}
	recoverReply := pigtest.Reply{FromRequest: func(request map[string]any) pigtest.Reply {
		handle := regexp.MustCompile(`cr1_[0-9a-f]{16}`).FindString(fmt.Sprint(request["messages"]))
		if handle == "" {
			return pigtest.HTTPError(400, "recovery handle missing")
		}
		return pigtest.Calls(pigtest.Call("context_recover", map[string]any{"handle": handle, "limit": 4096}))
	}}
	replies = append(replies, recoverReply, recoverReply, pigtest.Text("done"))
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"one long task"}})
	requests := mock.Requests()
	if len(requests) != 19 {
		t.Fatalf("want 19 requests, got %d; notices=%q stderr=%s", len(requests), result.Notices(), result.Stderr)
	}
	if !strings.Contains(fmt.Sprint(requests[17]["messages"]), "RECOVERY_ORIGINAL_NEEDLE") {
		t.Fatalf("recovery did not return the original: %v; notices=%q", requests[17]["messages"], result.Notices())
	}
	files, err := filepath.Glob(filepath.Join(home.AgentDir(), "sessions", "*", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("missing saved session: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	selected, retrieved, originalReads, recoveryResults := 0, 0, 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["customType"] == recoveryEntry {
			details, _ := entry["data"].(map[string]any)
			keys, _ := details["keys"].([]any)
			for _, value := range keys {
				key, _ := value.(string)
				if strings.HasPrefix(key, "selected/") {
					selected++
				}
				if strings.HasPrefix(key, "retrieved/") {
					retrieved++
				}
			}
		}
		message, _ := entry["message"].(map[string]any)
		if message["role"] != "toolResult" {
			continue
		}
		if message["toolName"] == "read" && strings.Contains(fmt.Sprint(message["content"]), "RECOVERY_ORIGINAL_NEEDLE") {
			originalReads++
		}
		if message["toolName"] == "context_recover" {
			if message["isError"] == true {
				t.Fatalf("recovery failed: %v", message)
			}
			if !strings.Contains(fmt.Sprint(message["content"]), "not current source") && !strings.Contains(fmt.Sprint(message["content"]), "Use read for current source") {
				t.Fatal("recovery granted current source authority")
			}
			recoveryResults++
		}
	}
	if selected != 1 || retrieved != 1 || originalReads != 1 || recoveryResults != 2 {
		t.Fatalf("selected=%d retrieved=%d reads=%d recovery results=%d", selected, retrieved, originalReads, recoveryResults)
	}
}
