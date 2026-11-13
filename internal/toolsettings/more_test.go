package toolsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResetRestoresCurrentDefaultPermissions(t *testing.T) {
	c := Config{Defaults: map[string]bool{"bash": true, "optional": false}, DisabledTools: map[string]bool{"bash": true}, EnabledTools: map[string]bool{"optional": true}, DisabledSkills: map[string]bool{"a": true}}
	c.Reset()
	if !c.ToolEnabled("bash") || !c.ToolEnabled("optional") || c.ToolEnabled("browser_navigate") || len(c.DisabledSkills) != 0 {
		t.Fatalf("reset=%+v", c)
	}
}

func TestSaveRejectsConcurrentEditsAndLockConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	initial, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	next, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	next.DisabledTools["bash"] = true
	if err := SaveIfUnchanged(path, initial, next); err != nil {
		t.Fatal(err)
	}
	if err := SaveIfUnchanged(path, initial, initial); err == nil || !strings.Contains(err.Error(), "another session") {
		t.Fatalf("conflict=%v", err)
	}
	got, err := Load(path)
	if err != nil || !got.DisabledTools["bash"] {
		t.Fatal("conflict replaced saved settings")
	}
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveIfUnchanged(path, next, initial); err == nil {
		t.Fatal("existing lock ignored")
	}
}

func TestLoadedSkillCountsUseSuppliedBlocksNotFiles(t *testing.T) {
	path := Canonical(filepath.Join(t.TempDir(), "a", "SKILL.md"))
	block := "<skill name=\"a\" location=\"" + path + "\">\né😀 body\n</skill>"
	messages := []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": block + "\n\narguments"}}},
		map[string]any{"role": "user", "content": block},
		map[string]any{"content": "mention a skill"},
	}
	counts := LoadedSkills(messages)
	n, _ := Characters(block)
	if len(counts) != 1 || counts[path] != 2*n {
		t.Fatalf("counts=%v want %d", counts, 2*n)
	}
	// A new context snapshot replaces the old counts after compaction.
	if counts := LoadedSkills(nil); len(counts) != 0 {
		t.Fatal(counts)
	}
	m := &Menu{LoadedKnown: true, Tabs: [2][]Row{nil, {{Name: "a", Enabled: false, LoadedChars: n}}}}
	m.Handle("?")
	text := strings.Join(m.Render(160), "\n")
	if !strings.Contains(text, "Last request loaded skill blocks:") || strings.Contains(text, "unknown until") {
		t.Fatal(text)
	}
}
