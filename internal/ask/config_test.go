package ask

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPrecedenceAndMalformedPrimary(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	legacy := filepath.Join(home, ".config", "rpiv-ask-user-question", "config.json")
	primary := filepath.Join(xdg, "rpiv-ask-user-question", "config.json")
	put := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := func(key string) string {
		switch key {
		case "HOME":
			return home
		case "XDG_CONFIG_HOME":
			return xdg
		}
		return ""
	}
	put(legacy, `{"collapseKey":"off","guidance":{"description":"Legacy"}}`)
	c := LoadConfig(env)
	if c.CollapseKey != "off" || c.Guidance.Description != "Legacy" {
		t.Fatalf("legacy fallback: %#v", c)
	}
	put(primary, `{"collapseKey":" ALT+O ","guidance":{"description":"Primary","promptGuidelines":["first","second"]}}`)
	c = LoadConfig(env)
	if c.CollapseKey != "alt+o" || c.Guidance.Description != "Primary" || len(c.Guidance.Guidelines) != 2 {
		t.Fatalf("primary: %#v", c)
	}
	put(primary, `{"guidance":`)
	c = LoadConfig(env)
	if c.Warning == "" || c.CollapseKey != "ctrl+]" || c.Guidance.Description == "Legacy" {
		t.Fatalf("malformed primary must warn without legacy fallback: %#v", c)
	}
}
func TestInvalidConfigFieldsKeepDefaults(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "rpiv-ask-user-question")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"collapseKey":"ctrl+ctrl+a","guidance":{"description":"","promptGuidelines":["valid",42]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := LoadConfig(func(key string) string {
		if key == "HOME" {
			return home
		}
		if key == "XDG_CONFIG_HOME" {
			return "relative"
		}
		return ""
	})
	if c.CollapseKey != "ctrl+]" || c.Guidance.Description == "" || c.Guidance.Guidelines[0] == "valid" {
		t.Fatalf("invalid fields changed defaults: %#v", c)
	}
}
