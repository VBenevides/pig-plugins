package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func settingsPath(t *testing.T) string { return filepath.Join(t.TempDir(), "nested", SettingsFileName) }

func readObject(t *testing.T, file string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMissingFileMeansInteractiveAndModeRoundTripsKeepingOtherKeys(t *testing.T) {
	file := settingsPath(t)
	if got := LoadSettings(file); got != (Settings{Mode: Interactive}) {
		t.Fatalf("missing file = %+v", got)
	}
	if err := SaveSettings(file, Change{Mode: new(Strict)}); err != nil {
		t.Fatal(err)
	}
	if got := LoadSettings(file); got != (Settings{Mode: Strict}) {
		t.Fatalf("after save = %+v", got)
	}
	if err := os.WriteFile(file, []byte(`{"mode":"strict","note":"mine","big":12345678901234567890}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(file, Change{Mode: new(Interactive)}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "12345678901234567890") {
		t.Errorf("a number was rewritten: %s", data)
	}
	if got := readObject(t, file); got["mode"] != "interactive" || got["note"] != "mine" {
		t.Errorf("file = %v", got)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestDamagedFileFailsClosedToStrictWithAReason(t *testing.T) {
	file := settingsPath(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"{nope", "[1]", "null", `{"mode": "auto"}`, `{"mode": 3}`, `{"lancet": true}`,
		`{"lancet": {"enabled": "yes"}}`, `{"mode":"strict"} trailing`, ""} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		got := LoadSettings(file)
		if got.Mode != Strict || got.Lancet || !strings.Contains(got.Problem, file) {
			t.Errorf("%q => %+v, want strict, lancet off, a problem naming the file", text, got)
		}
	}
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadSettings(file); got != (Settings{Mode: Interactive}) {
		t.Errorf("empty object = %+v", got)
	}
}

func TestUnreadableFileFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), SettingsFileName)
	if err := os.Mkdir(dir, 0o755); err != nil { // a directory cannot be read as a file
		t.Fatal(err)
	}
	got := LoadSettings(dir)
	if got.Mode != Strict || !strings.Contains(got.Problem, "is unreadable") {
		t.Errorf("got %+v", got)
	}
}

func TestSavingOverANonObjectRefusesAndLeavesTheFile(t *testing.T) {
	file := settingsPath(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"[1]", "{broken"} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := SaveSettings(file, Change{Mode: new(Strict)}); err == nil {
			t.Errorf("%q: save succeeded", text)
		}
		if data, _ := os.ReadFile(file); string(data) != text {
			t.Errorf("%q was changed to %q", text, data)
		}
	}
}

func TestLancetEnabledUsesUpstreamShapeWithoutDisturbingOtherKeys(t *testing.T) {
	file := settingsPath(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"mode":"strict","lancet":{"enabled":false,"extra":1},"note":"mine"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(file, Change{Lancet: new(true)}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"mode": "strict", "lancet": map[string]any{"enabled": true, "extra": float64(1)}, "note": "mine"}
	if got := readObject(t, file); !reflect.DeepEqual(got, want) {
		t.Errorf("file = %v, want %v", got, want)
	}
	if got := LoadSettings(file); got != (Settings{Mode: Strict, Lancet: true}) {
		t.Errorf("load = %+v", got)
	}
	if err := SaveSettings(file, Change{Mode: new(Interactive)}); err != nil {
		t.Fatal(err)
	}
	if got := LoadSettings(file); got != (Settings{Mode: Interactive, Lancet: true}) {
		t.Errorf("mode change disturbed lancet: %+v", got)
	}
	fresh := settingsPath(t)
	if err := SaveSettings(fresh, Change{Lancet: new(true)}); err != nil {
		t.Fatal(err)
	}
	if got := readObject(t, fresh); !reflect.DeepEqual(got, map[string]any{"lancet": map[string]any{"enabled": true}}) {
		t.Errorf("fresh file = %v", got)
	}
}

func TestSettingsFileFollowsTheAgentDirectory(t *testing.T) {
	env := map[string]string{"PIG_CODING_AGENT_DIR": "/a", "PIG_HOME": "/b", "HOME": "/c"}
	if got := SettingsFile(func(k string) string { return env[k] }); got != "/a/smart-approve-lancet.json" {
		t.Errorf("got %s", got)
	}
}
