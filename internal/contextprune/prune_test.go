package contextprune

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func assistantCall(id, name string, args map[string]any) map[string]any {
	return map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": id, "name": name, "arguments": args}}}
}

func result(id, name, text string) map[string]any {
	return map[string]any{"role": "toolResult", "toolCallId": id, "toolName": name, "isError": false, "content": []any{map[string]any{"type": "text", "text": text}}}
}

func user(text string) map[string]any {
	return map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}
}

func filler(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = user("filler")
	}
	return out
}

// small settings keep the fixtures short.
var small = Settings{KeepRecent: 2, MinSavingChars: 1, BashMaxChars: 100, BashHeadChars: 40, BashTailChars: 20}

func history(parts ...any) []any {
	var messages []any
	for _, part := range parts {
		messages = append(messages, part)
	}
	return append(messages, filler(small.KeepRecent)...)
}

func resultText(t *testing.T, message any) string {
	t.Helper()
	text, ok := plainText(message.(map[string]any))
	if !ok {
		t.Fatalf("message is not plain text: %v", message)
	}
	return text
}

func TestRepeatedReadOfTheSameRangeKeepsOnlyTheNewest(t *testing.T) {
	body := strings.Repeat("line\n", 100)
	messages := history(
		assistantCall("a", "read", map[string]any{"path": "src/a.go"}), result("a", "read", body),
		assistantCall("b", "read", map[string]any{"path": "./src/a.go"}), result("b", "read", body),
	)
	out, changed := Apply(messages, Decide(messages, small))
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if !strings.Contains(resultText(t, out[1]), "read again later") || resultText(t, out[3]) != body {
		t.Fatalf("older read must be pruned and the newest kept: %q / %q", resultText(t, out[1]), resultText(t, out[3]))
	}
}

func TestReadOfAnotherRangeIsKept(t *testing.T) {
	body := strings.Repeat("line\n", 100)
	messages := history(
		assistantCall("a", "read", map[string]any{"path": "a.go", "offset": 1, "limit": 50}), result("a", "read", body),
		assistantCall("b", "read", map[string]any{"path": "a.go", "offset": 51, "limit": 50}), result("b", "read", body),
	)
	if got := Decide(messages, small); len(got) != 0 {
		t.Fatalf("different ranges show different lines: %v", got)
	}
}

func TestLaterEditMakesEarlierReadAndEditStale(t *testing.T) {
	body := strings.Repeat("line\n", 100)
	messages := history(
		assistantCall("r", "read", map[string]any{"path": "a.go"}), result("r", "read", body),
		assistantCall("e1", "edit", map[string]any{"path": "a.go"}), result("e1", "edit", "Applied 1 edit(s) to a.go\n"+body),
		assistantCall("e2", "edit", map[string]any{"path": "a.go"}), result("e2", "edit", "Applied 1 edit(s) to a.go\n"+body),
	)
	decisions := Decide(messages, small)
	if _, ok := decisions["r"]; !ok {
		t.Error("read before an edit is stale")
	}
	if _, ok := decisions["e1"]; !ok {
		t.Error("earlier edit result is stale after a later edit")
	}
	if _, ok := decisions["e2"]; ok {
		t.Error("the newest edit result must stay")
	}
}

func TestTodoResultsKeepOnlyTheNewest(t *testing.T) {
	list := strings.Repeat("Remaining item\n", 20)
	messages := history(
		assistantCall("t1", "todo", nil), result("t1", "todo", list),
		assistantCall("t2", "todo", nil), result("t2", "todo", list),
	)
	decisions := Decide(messages, small)
	if _, ok := decisions["t1"]; !ok || len(decisions) != 1 {
		t.Fatalf("only the older todo result is replaced: %v", decisions)
	}
}

func TestLongShellOutputKeepsHeadAndTailOutsideTheRecentWindow(t *testing.T) {
	long := strings.Repeat("é", 200) // Two bytes per rune: a cut inside a rune would break the text.
	messages := history(assistantCall("b", "bash", map[string]any{"command": "x"}), result("b", "bash", long))
	out, _ := Apply(messages, Decide(messages, small))
	text := resultText(t, out[1])
	if !utf8.ValidString(text) || !strings.Contains(text, "[... tool result middle pruned ...]") || len(text) >= len(long) {
		t.Fatalf("bad truncation: %q", text)
	}
	if !strings.HasPrefix(text, "éé") || !strings.HasSuffix(text, "éé") {
		t.Fatalf("head and tail must stay: %q", text)
	}
}

func TestRecentMessagesErrorsAndImagesAreNeverChanged(t *testing.T) {
	long := strings.Repeat("x", 500)
	failed := result("f", "bash", long)
	failed["isError"] = true
	image := result("i", "bash", long)
	image["content"] = []any{map[string]any{"type": "text", "text": long}, map[string]any{"type": "image", "data": "..."}}
	recent := []any{assistantCall("n", "bash", map[string]any{"command": "x"}), result("n", "bash", long)}
	messages := append(history(
		assistantCall("f", "bash", nil), failed,
		assistantCall("i", "bash", nil), image,
	), recent...)
	if got := Decide(messages, Settings{KeepRecent: 2, MinSavingChars: 1, BashMaxChars: 100, BashHeadChars: 40, BashTailChars: 20}); len(got) != 0 {
		t.Fatalf("nothing may change: %v", got)
	}
}

func TestSmallSavingIsNotWorthBreakingTheCache(t *testing.T) {
	body := strings.Repeat("line\n", 100)
	messages := history(
		assistantCall("a", "read", map[string]any{"path": "a.go"}), result("a", "read", body),
		assistantCall("b", "read", map[string]any{"path": "a.go"}), result("b", "read", body),
	)
	settings := small
	settings.MinSavingChars = 10000
	if got := Decide(messages, settings); len(got) != 0 {
		t.Fatalf("saving below the minimum must prune nothing: %v", got)
	}
}

func TestRepeatedSkillBodyKeepsTheNewest(t *testing.T) {
	skill := `<skill name="todo-solver" location="/x">` + strings.Repeat("rules ", 100)
	messages := history(user(skill), user("work"), user(skill))
	out, changed := Apply(messages, Decide(messages, small))
	if changed != 1 || !strings.Contains(resultText(t, out[0]), "loaded again later") || resultText(t, out[2]) != skill {
		t.Fatalf("older skill body must be replaced: changed=%d %q", changed, resultText(t, out[0]))
	}
}

func TestApplyDoesNotChangeItsInputAndIsIdempotent(t *testing.T) {
	body := strings.Repeat("line\n", 100)
	messages := history(
		assistantCall("a", "read", map[string]any{"path": "a.go"}), result("a", "read", body),
		assistantCall("b", "read", map[string]any{"path": "a.go"}), result("b", "read", body),
	)
	before := resultText(t, messages[1])
	out, _ := Apply(messages, Decide(messages, small))
	if resultText(t, messages[1]) != before {
		t.Fatal("input message was changed")
	}
	again := Decide(out, small)
	if len(again) != 0 {
		t.Fatalf("a second pass must find nothing new: %v", again)
	}
	if out[1].(map[string]any)["toolCallId"] != "a" || out[1].(map[string]any)["role"] != "toolResult" {
		t.Fatal("call pairing fields must stay")
	}
}

func TestStaleDecisionsDoNotTouchUnknownMessages(t *testing.T) {
	messages := []any{user("hello"), result("gone", "read", "text")}
	out, changed := Apply(messages, Decisions{"gone": {Text: "x", Saved: 1}, SkillKey(0): {Text: "x", Saved: 1}})
	if changed != 0 || resultText(t, out[1]) != "text" || resultText(t, out[0]) != "hello" {
		t.Fatalf("messages without a call or skill body stay: changed=%d", changed)
	}
}
