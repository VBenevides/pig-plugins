package toolsettings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPromptStylePersistenceAndProfileIndependence(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg, err := Load(path)
	if err != nil || cfg.Style() != "default" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg.PromptStyle = "LEGACY"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil || cfg.Style() != "legacy" {
		t.Fatalf("style=%s err=%v", cfg.Style(), err)
	}
	for _, profile := range []string{DefaultPreset, MinimumPreset, NativePreset, BenchmarkPreset} {
		t.Setenv(ProfileEnv, profile)
		effective, err := LoadEffective(path)
		if err != nil || effective.Style() != "legacy" {
			t.Fatalf("profile=%s style=%s err=%v", profile, effective.Style(), err)
		}
	}
	menu := &Menu{PromptStyle: "legacy"}
	menu.ApplyPreset(BenchmarkPreset)
	menu.ApplyTo(&cfg)
	if cfg.Style() != "legacy" {
		t.Fatal("tool preset changed prompt style")
	}
}

func TestPromptStyleMenuCancelAndLockedTools(t *testing.T) {
	m := &Menu{PromptStyle: "default", ProfileOverride: BenchmarkPreset, Tabs: [2][]Row{{{ID: "bash", Enabled: true}}, nil}}
	for _, key := range []string{"space", "p", "r"} {
		m.Handle(key)
	}
	if !m.Tabs[0][0].Enabled || m.PresetPending || m.ResetPending {
		t.Fatal("launch tool profile was editable")
	}
	m.Handle("S")
	if m.PromptStyle != "legacy" || !strings.Contains(strings.Join(m.Render(96), "\n"), "Prompt style: legacy") {
		t.Fatal("style toggle missing")
	}
	done, save := m.Handle("escape")
	if !done || save {
		t.Fatal("cancel saved settings")
	}
	cfg := Config{PromptStyle: "default", DisabledTools: map[string]bool{"x": true}, ToolAllowlist: []string{"x"}}
	before := cfg
	m.ApplyTo(&cfg)
	if cfg.Style() != "legacy" || !reflect.DeepEqual(cfg.DisabledTools, before.DisabledTools) || !reflect.DeepEqual(cfg.ToolAllowlist, before.ToolAllowlist) {
		t.Fatalf("locked save changed permissions: %+v", cfg)
	}
	m.Handle("s")
	if m.PromptStyle != "default" {
		t.Fatal("cannot return to default")
	}
}

func TestInvalidPromptStyleIsObservableAndDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{"promptStyle":"typo"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid style accepted")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Config{PromptStyle: "typo"}); err == nil {
		t.Fatal("invalid style saved")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid save overwrote settings")
	}
}

func TestConcurrentStyleChoiceIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Save(path, Config{PromptStyle: "default"}); err != nil {
		t.Fatal(err)
	}
	previous, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	next := previous
	next.PromptStyle = "legacy"
	if err := Save(path, next); err != nil {
		t.Fatal(err)
	}
	if err := SaveIfUnchanged(path, previous, previous); err == nil {
		t.Fatal("concurrent prompt-style change overwritten")
	}
	actual, err := Load(path)
	if err != nil || actual.Style() != "legacy" {
		t.Fatalf("style=%s err=%v", actual.Style(), err)
	}
}
