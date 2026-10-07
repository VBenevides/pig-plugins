package hashline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testdata/ts-golden.json was produced by running the TypeScript harness-hashline extension
// (.agent-work/scripts/hashline-golden/generate.mjs); these tests require byte-identical Go behavior.

type goldenOutcome struct {
	Text    *string        `json:"text"`
	Details map[string]any `json:"details"`
	Error   *string        `json:"error"`
}

type golden struct {
	ReadFiles   map[string]string `json:"readFiles"`
	ReadResults []struct {
		Name    string         `json:"name"`
		File    string         `json:"file"`
		Params  map[string]any `json:"params"`
		Outcome goldenOutcome  `json:"outcome"`
	} `json:"readResults"`
	EditResults []struct {
		Name    string        `json:"name"`
		File    string        `json:"file"`
		Content string        `json:"content"`
		Edits   any           `json:"edits"`
		Outcome goldenOutcome `json:"outcome"`
		After   string        `json:"after"`
	} `json:"editResults"`
	OutlineResults []struct {
		Name    string   `json:"name"`
		Lines   []string `json:"lines"`
		Symbols []struct {
			Start int    `json:"start"`
			End   int    `json:"end"`
			Kind  string `json:"kind"`
			Name  string `json:"name"`
			Depth int    `json:"depth"`
		} `json:"symbols"`
		Rendered string `json:"rendered"`
	} `json:"outlineResults"`
	Hashes []struct {
		Text   string `json:"text"`
		Hash   string `json:"hash"`
		Anchor string `json:"anchor"`
	} `json:"hashes"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ts-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// roundTrip converts details to what the golden JSON holds.
func roundTrip(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func compareOutcome(t *testing.T, want goldenOutcome, got Result, err error, scrub func(string) string) {
	t.Helper()
	if want.Error != nil {
		if err == nil {
			t.Fatalf("want error %q, got result %q", *want.Error, got.Text)
		}
		if message := scrub(err.Error()); message != *want.Error {
			t.Fatalf("error mismatch\nwant: %s\n got: %s", *want.Error, message)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text := scrub(got.Text); text != *want.Text {
		t.Fatalf("text mismatch\nwant: %q\n got: %q", *want.Text, text)
	}
	if !reflect.DeepEqual(roundTrip(t, got.Details), roundTrip(t, want.Details)) {
		t.Fatalf("details mismatch\nwant: %v\n got: %v", want.Details, got.Details)
	}
}

func TestLineHashAgainstTypeScript(t *testing.T) {
	for _, c := range loadGolden(t).Hashes {
		if got := LineHash(c.Text); got != c.Hash {
			t.Errorf("LineHash(%q) = %s, want %s", c.Text, got, c.Hash)
		}
	}
}

func TestReadAgainstTypeScript(t *testing.T) {
	g := loadGolden(t)
	dir := t.TempDir()
	for name, content := range g.ReadFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range g.ReadResults {
		t.Run(c.Name, func(t *testing.T) {
			params := map[string]any{}
			for key, value := range c.Params {
				if text, ok := value.(string); ok {
					value = strings.ReplaceAll(text, "<CWD>", dir)
				}
				params[key] = value
			}
			got, err := Read(dir, params)
			compareOutcome(t, c.Outcome, got, err, func(s string) string { return strings.ReplaceAll(s, dir, "<CWD>") })
		})
	}
}

func TestEditAgainstTypeScript(t *testing.T) {
	g := loadGolden(t)
	for _, c := range g.EditResults {
		t.Run(c.Name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, c.File)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(c.Content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Edit(dir, map[string]any{"path": c.File, "edits": c.Edits})
			compareOutcome(t, c.Outcome, got, err, func(s string) string { return strings.ReplaceAll(s, dir, "<CWD>") })
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != c.After {
				t.Fatalf("file after edit\nwant: %q\n got: %q", c.After, after)
			}
			leftovers, _ := filepath.Glob(filepath.Join(dir, "*", ".*.tmp"))
			top, _ := filepath.Glob(filepath.Join(dir, ".*.tmp"))
			if len(leftovers)+len(top) > 0 {
				t.Fatalf("temporary files left behind: %v %v", leftovers, top)
			}
		})
	}
}

func TestOutlineAgainstTypeScript(t *testing.T) {
	for _, c := range loadGolden(t).OutlineResults {
		t.Run(c.Name, func(t *testing.T) {
			symbols, err := BuildOutline(c.Name, c.Lines)
			if err != nil {
				t.Fatal(err)
			}
			if len(symbols) != len(c.Symbols) {
				t.Fatalf("symbol count: got %d, want %d\n got: %+v", len(symbols), len(c.Symbols), symbols)
			}
			for i, want := range c.Symbols {
				got := symbols[i]
				if got.Start != want.Start || got.End != want.End || got.Kind != want.Kind || got.Name != want.Name || got.Depth != want.Depth {
					t.Fatalf("symbol %d: got %+v, want %+v", i, got, want)
				}
			}
			if len(symbols) > 0 {
				if got := RenderOutline(symbols); got != c.Rendered {
					t.Fatalf("rendered outline\nwant: %q\n got: %q", c.Rendered, got)
				}
			}
		})
	}
}
