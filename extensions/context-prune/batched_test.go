package contextprune

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	prune "github.com/VBenevides/pig-plugins/internal/contextprune"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestEightTurnBatchPrunesBelowCompactionLimit(t *testing.T) {
	st := &state{enabled: true, fresh: true}
	early := prunableHistory()[:3]
	st.apply(early, func() bool { return false })
	full := prunableHistory()
	for turn := 1; turn <= RefreshEveryTurns; turn++ {
		st.turnsSinceRefresh++
		out := st.apply(full, func() bool { return false })
		if (out != nil) != (turn == RefreshEveryTurns) {
			t.Fatalf("turn %d: only the eighth turn may refresh", turn)
		}
	}
	if st.turnsSinceRefresh != 0 || st.decisions["one"].Text == "" {
		t.Fatal("batch must reset its cadence and mask the old read")
	}
}

func TestRefreshRequiresAdditionalSavingsAndDoesNotRestoreOldOutput(t *testing.T) {
	st := &state{enabled: true, fresh: true}
	messages := prunableHistory()
	st.apply(messages, func() bool { return false })
	previous := prune.Decisions{}
	for key, decision := range st.decisions {
		previous[key] = decision
	}
	if len(previous) == 0 {
		t.Fatal("fixture must start with a large pruning decision")
	}
	appendRead := func(id, path string, size int) {
		messages = append(messages,
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": id, "name": "read", "arguments": map[string]any{"path": path}}}},
			map[string]any{"role": "toolResult", "toolCallId": id, "content": []any{map[string]any{"type": "text", "text": strings.Repeat("x", size)}}},
		)
	}
	appendRead("small-one", "small.txt", 2000)
	appendRead("small-two", "small.txt", 2000)
	for i := range 11 {
		appendRead(fmt.Sprintf("filler-%d", i), "filler.txt", 1)
	}
	st.turnsSinceRefresh = RefreshEveryTurns
	st.apply(messages, func() bool { return false })
	st.apply(messages, func() bool { return true })
	if !reflect.DeepEqual(st.decisions, previous) {
		t.Fatal("previous savings must not justify a small additional cache invalidation")
	}
	appendRead("large-one", "large.txt", 40000)
	appendRead("large-two", "large.txt", 40000)
	for i := range 11 {
		appendRead(fmt.Sprintf("new-filler-%d", i), "filler.txt", 1)
	}
	st.turnsSinceRefresh = RefreshEveryTurns
	st.apply(messages, func() bool { return false })
	if st.decisions["large-one"].Text == "" || st.decisions["one"].Text != previous["one"].Text {
		t.Fatal("a large new batch must add decisions without restoring old output")
	}
}

func TestProviderPrunesDuringOneRunWithoutSummaryAndKeepsSavedOutput(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	body := strings.Repeat("UNIQUE_OLD_READ "+strings.Repeat("x", 30)+"\n", 1000)
	if err := os.WriteFile(filepath.Join(home.Work, "old.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "tiny.txt"), []byte("tiny\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replies := []pigtest.Reply{pigtest.Calls(pigtest.Call("read", map[string]any{"path": "old.txt"}))}
	for range 15 {
		replies = append(replies, pigtest.Calls(pigtest.Call("read", map[string]any{"path": "tiny.txt"})))
	}
	replies = append(replies, pigtest.Text("done"))
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{"one long task"}})
	requests := mock.Requests()
	if len(requests) != 17 {
		t.Fatalf("want 16 tool turns and one answer without summary; got %d; notices %q; stderr %s", len(requests), result.Notices(), result.Stderr)
	}
	for i, request := range requests {
		text := fmt.Sprint(request["messages"])
		if strings.Contains(text, "ONLY output the structured summary") {
			t.Fatal("pruning must not request an LLM summary")
		}
		if i < 16 && strings.Contains(text, "[pruned:") {
			t.Fatalf("request %d pruned before the first eligible eight-turn batch", i)
		}
		if i == 16 && (!strings.Contains(text, "[recovery: toolResult") || strings.Contains(text, "UNIQUE_OLD_READ")) {
			t.Fatal("sixteenth tool turn must mask the unique old read with a recovery reference")
		}
	}
	files, err := filepath.Glob(filepath.Join(home.AgentDir(), "sessions", "*", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("want one saved session: %v %v", files, err)
	}
	saved, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "UNIQUE_OLD_READ") || strings.Contains(string(saved), "[recovery: toolResult") {
		t.Fatal("provider masking must not change the saved tool output")
	}
}
