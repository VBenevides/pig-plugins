package promptcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCuratorIsNotBundled(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join("..", "..", "piglet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "pi-curator") {
		t.Fatal("curator remains in the binary manifest")
	}
	for _, name := range []string{"memory_search", "memory_read", "recall.md"} {
		if strings.Contains(load(t)["SYSTEM.md"], name) {
			t.Fatalf("default instructions still request %s", name)
		}
	}
}
