package contextprune

import (
	"strings"
	"testing"
)

func TestComposeSeparatesCategoriesAndRecoveryOverhead(t *testing.T) {
	failed := result("f", "bash", "compile failed")
	failed["isError"] = true
	view := []any{
		user("hello"),
		assistantCall("a", "write", map[string]any{"path": "x"}), result("a", "write", strings.Repeat("x", 50)),
		assistantCall("f", "bash", map[string]any{}), failed,
		assistantCall("o", "read", map[string]any{}), result("o", "read", "[pruned: old \"read\" result, 10 bytes; status unchanged; recovery: cr1_x; use context_recover]"),
		map[string]any{"role": "toolResult", "toolCallId": "i", "content": []any{map[string]any{"type": "image", "data": "x"}}},
	}
	got := Compose(view, 6)
	if got.UserText != 5 || got.CallArguments != len(`{"path":"x"}`)+2+2 || got.ResultText != 50 || got.FailureText != 14 || got.RecoveryNotices == 0 || got.Media != 1 {
		t.Fatalf("unexpected composition: %+v", got)
	}
	if got.ResultsTotal != 4 || got.ResultsReduced != 1 || got.ResultsProtected != 1 || got.ProtectedResults != got.RecoveryNotices {
		t.Fatalf("unexpected counts: %+v", got)
	}
}
