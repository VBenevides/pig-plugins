package imageviewext

import "testing"

func TestNumberingRestoresCurrentBranch(t *testing.T) {
	entry := func(role string, content any) map[string]any {
		return map[string]any{"type": "message", "message": map[string]any{"role": role, "content": content}}
	}
	branch := []map[string]any{entry("user", "look [Image #2]"), entry("toolResult", []any{map[string]any{"type": "text", "text": "[Image #7]"}}), entry("assistant", "[Image #999]"), entry("user", []any{map[string]any{"type": "image", "data": "[Image #800]"}})}
	if got := nextImageNumber(branch); got != 8 {
		t.Fatalf("resume=%d", got)
	}
	if got := nextImageNumber(branch[:1]); got != 3 {
		t.Fatalf("fork=%d", got)
	}
	if got := nextImageNumber(nil); got != 1 {
		t.Fatalf("new session=%d", got)
	}
}
