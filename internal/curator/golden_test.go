package curator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTypeScriptParity(t *testing.T) {
	var g struct {
		SafeID []struct{ Raw, ID string } `json:"safeId"`
		Events []struct {
			Name     string
			Messages []any
			Events   []EventIn
		}
		Boundaries []struct {
			Messages any
			Event    EventIn
		}
		Parse []struct {
			Key, Value, Error string
			Result            map[string]any
		}
		Files []struct {
			Name     string
			Text     *string
			Error    string
			Loaded   map[string]any
			Describe []struct {
				Env   map[string]string
				Lines []string
			}
		}
		Prefetch []struct {
			Prompt string
			Words  []string
		}
		Startup []struct {
			Raw   string
			Count int
		} `json:"startupCount"`
	}
	data, err := os.ReadFile("testdata/ts-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	for _, c := range g.SafeID {
		if got := SafeID(c.Raw); got != c.ID {
			t.Errorf("SafeID(%q)=%q want %q", c.Raw, got, c.ID)
		}
	}
	for _, c := range g.Events {
		t.Run(c.Name, func(t *testing.T) {
			var got []EventIn
			calls := NewCallPaths(0)
			for _, m := range c.Messages {
				got = append(got, EventsFromMessage("sess1", m, calls)...)
			}
			if len(got) == 0 && len(c.Events) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.Events) {
				t.Errorf("events=%+v want %+v", got, c.Events)
			}
		})
	}
	for _, c := range g.Boundaries {
		if got := TaskBoundary("sess1", c.Messages); !reflect.DeepEqual(got, c.Event) {
			t.Errorf("boundary=%+v want %+v", got, c.Event)
		}
	}
	for _, c := range g.Parse {
		got, err := ParseSetting(c.Key, c.Value)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("parse %s %q: %v want %s", c.Key, c.Value, err, c.Error)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(settingsMap(got), c.Result) {
			t.Errorf("parse %s %q=%v want %v", c.Key, c.Value, settingsMap(got), c.Result)
		}
	}
	for _, c := range g.Files {
		t.Run("file/"+c.Name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "pi-curator.json")
			if c.Text != nil {
				if err := os.WriteFile(file, []byte(*c.Text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := LoadSettings(file)
			if (err != nil) != (c.Error != "") {
				t.Fatalf("load error=%v expected error=%s", err, c.Error)
			}
			if err == nil && !reflect.DeepEqual(settingsMap(got), c.Loaded) {
				t.Errorf("loaded=%v want %v", settingsMap(got), c.Loaded)
			}
			for _, d := range c.Describe {
				config := Config{Getenv: func(key string) string {
					if key == "PIG_CODING_AGENT_DIR" {
						return dir
					}
					return d.Env[key]
				}}
				if got := config.Describe(); !reflect.DeepEqual(got, d.Lines) {
					t.Errorf("describe=%v want %v", got, d.Lines)
				}
			}
		})
	}
	for _, c := range g.Prefetch {
		got := PrefetchWords(c.Prompt)
		if len(got) == 0 && len(c.Words) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.Words) {
			t.Errorf("words %q=%v want %v", c.Prompt, got, c.Words)
		}
	}
	for _, c := range g.Startup {
		if got := StartupCount(c.Raw); got != c.Count {
			t.Errorf("startup %q=%d want %d", c.Raw, got, c.Count)
		}
	}
}
func settingsMap(s Settings) map[string]any {
	m := map[string]any{}
	if s.Prefetch != nil {
		m["prefetchBudget"] = float64(*s.Prefetch)
	}
	if s.Startup != nil {
		m["startupDecisions"] = float64(*s.Startup)
	}
	if s.Engine != "" {
		m["searchEngine"] = s.Engine
	}
	return m
}
