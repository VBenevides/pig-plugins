package pigplugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/footerstatus"
)

func TestInteractionPageStatus(t *testing.T) {
	defer SetInteractionPage(0, 0)
	for _, tc := range []struct {
		current, total int
		want           string
	}{
		{1, 3, "1/3"}, {3, 3, "3/3"}, {0, 0, ""}, {4, 3, ""},
	} {
		SetInteractionPage(tc.current, tc.total)
		if got := footerstatus.Snapshot()["interaction-page"]; got != tc.want {
			t.Fatalf("page %d/%d = %q, want %q", tc.current, tc.total, got, tc.want)
		}
	}
}

func TestReadStoredImage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIG_CODING_AGENT_DIR", dir)
	hash := strings.Repeat("a", 64) + ".png"
	ref := "image-view://sha256/" + hash
	if _, _, err := ReadStoredImage(ref); err == nil {
		t.Fatal("missing blob accepted")
	}
	root := filepath.Join(dir, "image-view", "blobs")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, hash), []byte("saved bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _, err := ReadStoredImage(ref)
	if err != nil || data != "c2F2ZWQgYnl0ZXM=" {
		t.Fatalf("read blob: %q, %v", data, err)
	}
	for _, invalid := range []string{"file:///etc/passwd", "https://example.com/a.png", ref + "/../other", "prefix" + ref} {
		if _, _, err := ReadStoredImage(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
