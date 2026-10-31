package contextprune

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestManualPruningBypassesCadenceAndMinimumSavings(t *testing.T) {
	messages := prunableHistory()
	for _, index := range []int{2, 4} {
		message := messages[index].(map[string]any)
		message["content"] = []any{map[string]any{"type": "text", "text": strings.Repeat("small", 200)}}
	}
	st := &state{enabled: true, fresh: true}
	if st.apply(messages, func() bool { return false }) != nil {
		t.Fatal("automatic pruning must honor the minimum savings")
	}
	st.forcePending = true
	if st.apply(messages, func() bool { return false }) == nil || st.forcePending {
		t.Fatal("manual pruning must bypass cadence and minimum savings once")
	}
	protected := &state{enabled: true, fresh: true, forcePending: true}
	if protected.apply(messages[:5], func() bool { return false }) != nil {
		t.Fatal("manual pruning must not bypass recent-result protection")
	}
}

func TestNowCommandPreparesSmallPruneWithoutLLMCall(t *testing.T) {
	pigtest.RequirePig(t)
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprint(manual), func(t *testing.T) {
			home := pigtest.NewHome(t)
			if err := os.WriteFile(filepath.Join(home.Work, "old.txt"), []byte(strings.Repeat("MANUAL_OLD_OUTPUT\n", 100)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home.Work, "tiny.txt"), []byte("tiny\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			replies := []pigtest.Reply{pigtest.Calls(pigtest.Call("read", map[string]any{"path": "old.txt"}))}
			for range 11 {
				replies = append(replies, pigtest.Calls(pigtest.Call("read", map[string]any{"path": "tiny.txt"})))
			}
			replies = append(replies, pigtest.Text("first done"), pigtest.Text("second done"))
			mock := pigtest.NewMockLLM(replies...)
			defer mock.Close()
			prompts := []string{"task one"}
			if manual {
				prompts = append(prompts, "/context-prune now", "/context-prune status")
			}
			prompts = append(prompts, "task two")
			result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: prompts})
			requests := mock.Requests()
			if len(requests) != 14 {
				t.Fatalf("manual pruning must not call the LLM: requests=%d stderr=%s", len(requests), result.Stderr)
			}
			last := fmt.Sprint(requests[13]["messages"])
			if strings.Contains(last, "MANUAL_OLD_OUTPUT") == manual {
				t.Fatalf("manual=%v: wrong provider-visible pruning; notices=%q", manual, result.Notices())
			}
			if manual {
				notices := strings.Join(result.Notices(), "\n")
				for _, want := range []string{"composition (message bytes", "call arguments", "results: ", "folding: off", "budgets: old failures 32768 bytes, old results 65536 bytes, old edit and write payloads 32768 bytes", "last request: "} {
					if !strings.Contains(notices, want) {
						t.Fatalf("status lacks %q: %q", want, notices)
					}
				}
				if !strings.Contains(notices, "now: prepared 1 pruned messages") || !strings.Contains(last, "[recovery: cr1_") || !strings.Contains(last, "use context_recover") {
					t.Fatalf("command must prepare the prune before the next run and retain recovery: %q", notices)
				}
			}
		})
	}
}

func TestNowCommandDoesNotEnablePruningOrCallLLM(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"/context-prune now", "/context-prune off", "/context-prune now", "/context-prune now extra"}})
	if len(mock.Requests()) != 0 {
		t.Fatal("commands must not call the LLM")
	}
	notices := strings.Join(result.Notices(), "\n")
	for _, want := range []string{"now: prepared 0 pruned messages", "pruning is off. Use /context-prune on", "usage: /context-prune"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("missing %q: %s", want, notices)
		}
	}
}
