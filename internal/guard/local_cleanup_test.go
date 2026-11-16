package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/lancet"
)

func TestLocalCleanupDeletesAnUnusedModelOnlyWhileLancetIsOff(t *testing.T) {
	for _, tc := range []struct {
		name             string
		enabled, unknown bool
		deleted          bool
	}{{"off", false, false, true}, {"on", true, false, false}, {"settings unreadable", false, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			agentDir := t.TempDir()
			model := lancet.ModelDirectory(agentDir)
			if err := os.MkdirAll(model, 0o700); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
			if err := os.WriteFile(filepath.Join(filepath.Dir(model), "last-used"), []byte(old), 0o600); err != nil {
				t.Fatal(err)
			}
			NewLocal(agentDir, func(string) string { return "" }, lancet.InstallOptions{}).CleanupUnused(tc.enabled, tc.unknown)
			if _, err := os.Stat(model); (err != nil) != tc.deleted {
				t.Fatalf("model deleted = %v, want %v", err != nil, tc.deleted)
			}
		})
	}
}
