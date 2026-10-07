package curator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func settingsConfig(dir string) Config {
	return Config{Getenv: func(key string) string {
		if key == "PIG_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
}

func TestEnabledDefaultsAndValidation(t *testing.T) {
	config := settingsConfig(t.TempDir())
	values, err := config.Effective()
	if err != nil || !values.Enabled || values.Startup != "2" || values.Prefetch != "512" {
		t.Fatal(values, err)
	}
	for _, text := range []string{`{"enabled":"false"}`, `{"enabled":0}`, `{"enabled":null}`} {
		if err := os.WriteFile(config.File(), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Effective(); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}

func TestEnabledCommandsSaveBeforeSwitchAndPreserveSettings(t *testing.T) {
	config := settingsConfig(t.TempDir())
	original := `{"prefetchBudget":700,"startupDecisions":3,"searchEngine":"fts","unrelated":{"n":9007199254740993}}`
	if err := os.WriteFile(config.File(), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var switched []bool
	controller := Controller{Config: config, OnEnabledChange: func(enabled bool) error {
		values, err := settingsConfig(filepath.Dir(config.File())).Effective()
		if err != nil || values.Enabled != enabled || values.Prefetch != "700" || values.Startup != "3" || values.Engine != "fts" {
			t.Fatalf("switch occurred before durable settings: %+v %v", values, err)
		}
		switched = append(switched, enabled)
		return nil
	}}
	for _, command := range []string{"off", "on", "off"} {
		controller.Handle(context.Background(), command, func(message, level string) {
			if level != "info" || !strings.Contains(message, "enabled:") {
				t.Fatal(message, level)
			}
		})
	}
	if !reflect.DeepEqual(switched, []bool{false, true, false}) {
		t.Fatal(switched)
	}
	data, err := os.ReadFile(config.File())
	if err != nil || !strings.Contains(string(data), "9007199254740993") {
		t.Fatal(string(data), err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil || len(saved) != 5 || string(saved["enabled"]) != "false" {
		t.Fatal(saved, err)
	}
	info, err := os.Stat(config.File())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
}

func TestEnabledSaveFailureDoesNotSwitchOrReportSuccess(t *testing.T) {
	dir := t.TempDir()
	config := settingsConfig(dir)
	if err := os.Mkdir(config.File(), 0700); err != nil {
		t.Fatal(err)
	}
	controller := Controller{Config: config, OnEnabledChange: func(bool) error {
		t.Fatal("switch after failed save")
		return nil
	}}
	for _, command := range []string{"off", "on", "status"} {
		var notices int
		controller.Handle(context.Background(), command, func(message, level string) {
			notices++
			if level != "error" || !strings.Contains(message, "pi-curator:") {
				t.Fatal(message, level)
			}
		})
		if notices != 1 {
			t.Fatal(notices)
		}
	}
}

func TestEnabledHelpAndCompletions(t *testing.T) {
	for _, command := range []string{"off", "on"} {
		if !strings.Contains(CommandHelp, command) {
			t.Fatal(CommandHelp)
		}
		if got := Completions(command); len(got) != 1 || got[0].Value != command {
			t.Fatal(got)
		}
	}
	if got := Completions("o"); len(got) != 2 {
		t.Fatal(got)
	}
}

func TestBudgetStartupPreservesWholeUsefulNeighborAndEnvelope(t *testing.T) {
	good := "2026-10-07: retain 雪 whole"
	want := "\n\n" + StartupHeader + "\n- " + good
	limit := len(want)
	got := StartupBlock([]string{strings.Repeat("雪", 3000), good}, limit)
	if got != want || len(got) != limit {
		t.Fatal(got, len(got), limit)
	}
	if got := StartupBlock([]string{good}, limit-1); got != "" {
		t.Fatal("partial record emitted", got)
	}
}
