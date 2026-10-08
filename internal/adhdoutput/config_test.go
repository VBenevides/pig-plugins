package adhdoutput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationReadsOnlyExistingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	config, err := LoadConfig(path, "embedded")
	must(t, err)
	if config.DefaultEnabled || !config.ShowStatus || config.Rules != "embedded" {
		t.Fatal(config)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("default configuration wrote a file")
	}
	must(t, os.WriteFile(filepath.Join(dir, "custom.md"), []byte("custom presentation rules"), 0600))
	must(t, os.WriteFile(path, []byte(`{"defaultEnabled":true,"showStatus":false,"rulesFile":"custom.md"}`), 0600))
	config, err = LoadConfig(path, "embedded")
	must(t, err)
	if !config.DefaultEnabled || config.ShowStatus || config.Rules != "custom presentation rules" {
		t.Fatal(config)
	}
	for _, content := range []string{`{"defaultEnabled":"on"}`, `{`, strings.Repeat(" ", 65<<10), `{"rulesFile":"missing.md"}`} {
		must(t, os.WriteFile(path, []byte(content), 0600))
		if _, err := LoadConfig(path, "embedded"); err == nil {
			t.Fatal("bad configuration accepted")
		}
	}
}

func TestStateAndContextAreDistinctAndStrict(t *testing.T) {
	h := newHost()
	h.append(map[string]any{"type": "custom", "customType": StateType, "data": map[string]any{"version": float64(1), "enabled": true}})
	for _, bad := range []map[string]any{
		{"type": "custom", "customType": StateType, "data": nil},
		{"type": "custom", "customType": StateType, "data": map[string]any{"version": float64(2), "enabled": false}},
	} {
		s := h.snapshot
		s.Branch = append(append([]map[string]any{}, s.Branch...), bad)
		bad["id"], bad["parentId"] = "bad", s.LeafID
		s.LeafID = "bad"
		if _, _, err := s.Choice(); err == nil {
			t.Fatal("malformed latest state fell back to earlier state")
		}
	}
	rules := RulesMessage("rules")
	messages := []map[string]any{{"role": "user", "customType": RulesType, "content": rules}, {"role": "compactionSummary", "content": rules}}
	if contextMarker(messages, rules).active {
		t.Fatal("quoted rules were treated as active custom message")
	}
	messages = append(messages, map[string]any{"role": "custom", "customType": RulesType, "content": []any{map[string]any{"type": "text", "text": rules}}})
	if !contextMarker(messages, rules).current {
		t.Fatal("SDK text blocks were not recognized")
	}
	messages = append(messages, map[string]any{"role": "custom", "customType": DisabledType, "content": Cancellation})
	if contextMarker(messages, rules).active {
		t.Fatal("latest disabled marker did not cancel rules")
	}
}

func TestSaveDefaultEnabledKeepsOtherFieldsAndRejectsDamagedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config.json")
	must(t, SaveDefaultEnabled(path, true))
	config, err := LoadConfig(path, "embedded")
	must(t, err)
	if !config.DefaultEnabled || !config.ShowStatus {
		t.Fatal(config)
	}
	must(t, os.WriteFile(path, []byte(`{"showStatus":false,"future":7}`), 0o600))
	must(t, SaveDefaultEnabled(path, true))
	data, err := os.ReadFile(path)
	must(t, err)
	for _, want := range []string{`"defaultEnabled": true`, `"showStatus": false`, `"future": 7`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s lacks %s", data, want)
		}
	}
	must(t, os.WriteFile(path, []byte(`{`), 0o600))
	if SaveDefaultEnabled(path, false) == nil {
		t.Fatal("damaged configuration was overwritten")
	}
	if got, _ := os.ReadFile(path); string(got) != `{` {
		t.Fatalf("damaged file changed: %s", got)
	}
}
