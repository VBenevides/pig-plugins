package hashline

import (
	"fmt"
	"testing"
)

func TestContentVersionExactBytes(t *testing.T) {
	if got := ContentVersion(nil); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty SHA-256 = %s", got)
	}
	seen := map[string]string{}
	for _, content := range []string{"a", "a\n", "a\r\n", "\ufeffa\n", "a \n", "a\nb\n", "a\nB\n"} {
		version := ContentVersion([]byte(content))
		if len(version) != 64 {
			t.Fatalf("version length = %d", len(version))
		}
		if previous, ok := seen[version]; ok {
			t.Fatalf("different exact bytes share a version: %q and %q", previous, content)
		}
		seen[version] = content
	}
}

func TestContentVersionDistinguishesShortAnchorCollision(t *testing.T) {
	seen := map[string]string{}
	// Pigeonhole bound: 65,537 distinct lines must collide in a 16-bit anchor.
	for i := 0; i <= 1<<16; i++ {
		text := fmt.Sprintf("collision fixture %d", i)
		hash := LineHash(text)
		if previous, ok := seen[hash]; ok {
			if ContentVersion([]byte(previous)) == ContentVersion([]byte(text)) {
				t.Fatal("full versions failed to distinguish colliding display anchors")
			}
			return
		}
		seen[hash] = text
	}
	t.Fatal("expected a short-anchor collision")
}
