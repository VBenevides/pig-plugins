package lancet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func storeWithModel(t *testing.T) (agentDir, model string) {
	t.Helper()
	agentDir = t.TempDir()
	model = ModelDirectory(agentDir)
	if err := os.MkdirAll(model, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(model, "model.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return agentDir, model
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func TestCleanupDeletesOnlyAnUnusedModelWhileOff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		age     time.Duration
		enabled bool
		broken  bool
		deleted bool
	}{
		{"unused for 8 days", 8 * 24 * time.Hour, false, false, true},
		{"unused for 6 days", 6 * 24 * time.Hour, false, false, false},
		{"enabled", 30 * 24 * time.Hour, true, false, false},
		{"config unreadable", 30 * 24 * time.Hour, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentDir, model := storeWithModel(t)
			if err := writeMarker(storeRoot(agentDir), base.Add(-tc.age)); err != nil {
				t.Fatal(err)
			}
			runtime := RuntimeDirectory(agentDir)
			if err := os.MkdirAll(runtime, 0o700); err != nil {
				t.Fatal(err)
			}
			var logged []string
			got := CleanupUnused(CleanupOptions{AgentDir: agentDir, Enabled: tc.enabled, ConfigUnreadable: tc.broken, Now: base, Log: func(s string) { logged = append(logged, s) }})
			if got != tc.deleted || exists(model) == tc.deleted {
				t.Fatalf("deleted = %v, model exists = %v, want deleted %v", got, exists(model), tc.deleted)
			}
			if !exists(runtime) {
				t.Error("cleanup removed the runtime directory")
			}
			if tc.deleted && (len(logged) != 1 || !strings.Contains(logged[0], model)) {
				t.Errorf("log = %q, want one line with the path", logged)
			}
		})
	}
}

func TestCleanupResetsAMissingOrInvalidMarkerWithoutDeleting(t *testing.T) {
	for name, content := range map[string]*string{"missing": nil, "invalid": new("garbage"), "future": new(base.Add(time.Hour).Format(time.RFC3339))} {
		t.Run(name, func(t *testing.T) {
			agentDir, model := storeWithModel(t)
			if content != nil {
				if err := os.WriteFile(markerPath(storeRoot(agentDir)), []byte(*content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if CleanupUnused(CleanupOptions{AgentDir: agentDir, Now: base}) || !exists(model) {
				t.Fatal("the model was deleted")
			}
			if used, ok := readLastUsed(storeRoot(agentDir), base); !ok || !used.Equal(base) {
				t.Fatalf("marker = %v, %v; want reset to now", used, ok)
			}
		})
	}
}

func TestCleanupRefusesASymbolicLinkModel(t *testing.T) {
	agentDir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root := storeRoot(agentDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ModelDirectory(agentDir)); err != nil {
		t.Skip(err)
	}
	if err := writeMarker(root, base.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if CleanupUnused(CleanupOptions{AgentDir: agentDir, Now: base}) || !exists(filepath.Join(outside, "keep")) {
		t.Fatal("cleanup followed a symbolic link")
	}
}

func TestTouchWritesOncePerHourAndOnlyForAnExistingStore(t *testing.T) {
	ResetTouchThrottle()
	empty := t.TempDir()
	TouchLastUsed(empty, base, nil)
	if exists(storeRoot(empty)) {
		t.Fatal("touch created the store directory")
	}
	agentDir, _ := storeWithModel(t)
	TouchLastUsed(agentDir, base, nil)
	TouchLastUsed(agentDir, base.Add(30*time.Minute), nil)
	if used, _ := readLastUsed(storeRoot(agentDir), base.Add(time.Hour)); !used.Equal(base) {
		t.Fatalf("marker = %v, want the first touch", used)
	}
	TouchLastUsed(agentDir, base.Add(2*time.Hour), nil)
	if used, _ := readLastUsed(storeRoot(agentDir), base.Add(3*time.Hour)); !used.Equal(base.Add(2 * time.Hour)) {
		t.Fatalf("marker = %v, want the later touch", used)
	}
	if date, ok := DeletionDate(agentDir, base.Add(3*time.Hour)); !ok || !date.Equal(base.Add(2*time.Hour+RetentionPeriod)) {
		t.Fatalf("deletion date = %v, %v", date, ok)
	}
}
