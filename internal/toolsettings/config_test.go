package toolsettings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigPersistenceAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.DisabledTools["bash"] = true
	c.DisabledSkills["/skills/a/SKILL.md"] = true
	c.EnabledTools["read"] = true
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(c, got) {
		t.Fatalf("load=%+v %v", got, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private settings: %v %v", info, err)
	}
	if err := os.WriteFile(path, []byte(`{"disabledTools":{"bash":"yes"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed choices accepted")
	}
	if err := Save(t.TempDir(), c); err == nil {
		t.Fatal("directory accepted as settings file")
	}
}

func TestFilterSkillsExcludesDisabledAndKeepsNeighbors(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "reference.md"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := Config{DisabledSkills: map[string]bool{Canonical(filepath.Join(root, "a", "SKILL.md")): true}}
	paths, err := FilterSkills([]string{root}, c)
	if err != nil || !reflect.DeepEqual(paths, []string{filepath.Join(root, "b")}) {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Join(root, "a"), link); err != nil {
		t.Fatal(err)
	}
	paths, err = FilterSkills([]string{link}, c)
	if err != nil || len(paths) != 0 {
		t.Fatalf("symlink bypass: %v %v", paths, err)
	}
	if _, err := FilterSkills([]string{filepath.Join(root, "missing")}, c); err == nil {
		t.Fatal("missing resource not reported")
	}
	// Discovery controls do not restrict ordinary file access.
	if _, err := os.ReadFile(filepath.Join(root, "a", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestMenuControlsAndCounts(t *testing.T) {
	m := &Menu{Tabs: [2][]Row{{{Name: "read", Enabled: true, Visible: true, Context: "12345"}}, {{Name: "skill", Enabled: false, Visible: true, Context: "é😀"}}}}
	m.Handle(" ")
	if m.Tabs[0][0].Enabled {
		t.Fatal("space did not toggle")
	}
	m.Handle("tab")
	m.Handle(" ")
	if !m.Tabs[1][0].Enabled {
		t.Fatal("skill not enabled")
	}
	text := strings.Join(m.Render(120), "\n")
	if !strings.Contains(text, "Skills: 2 chars / ~1 tokens") || !strings.Contains(text, "Tools: 0 chars / ~0 tokens") {
		t.Fatal(text)
	}
	m.Handle("r")
	if done, _ := m.Handle("n"); done || m.Reset || m.ResetPending {
		t.Fatal("reset cancel failed")
	}
	m.Handle("R")
	if done, save := m.Handle("y"); !done || !save || !m.Reset {
		t.Fatal("reset confirmation failed")
	}
	if done, save := (&Menu{}).Handle("escape"); !done || save {
		t.Fatal("cancel saved settings")
	}
	empty := &Menu{}
	for _, key := range []string{"up", "down", " ", "tab", "down", " "} {
		empty.Handle(key)
	}
	if n, tokens := Characters("12345"); n != 5 || tokens != 2 {
		t.Fatalf("counts=%d,%d", n, tokens)
	}
}
