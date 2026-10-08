package userresources

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func env(dir string) func(string) string {
	return func(k string) string {
		switch k {
		case "PIG_CODING_AGENT_DIR":
			return dir
		case "HOME":
			return "/home/u"
		}
		return ""
	}
}

func TestDefaultsIncludeExistingDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(env(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"skillPaths": {filepath.Join(dir, "skills")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestConfigReplacesDefaultsAndExpandsPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"skillPaths": ["mine", "~/shared", "/abs"]}`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(env(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"skillPaths": {filepath.Join(dir, "mine"), "/home/u/shared", "/abs"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEmptyConfiguredListDisablesDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(`{"skillPaths": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(env(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

func TestMalformedConfigIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(`{nope`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(env(dir)); err == nil {
		t.Fatal("expected parse error")
	}
}
