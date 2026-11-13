package pathcompleteext

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"os"
	"path/filepath"
	"testing"
)

func TestFindToken(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		ok    bool
	}{
		{"path in text", []string{"look at /etc/ho"}, true},
		{"command name", []string{"/mo"}, false},
		{"bare slash after text", []string{"asd /"}, false},
		{"double slash", []string{"asd //"}, false},
		{"absolute path on first line", []string{"/etc/ho"}, true},
		{"fs arguments", []string{"/fs ~/My Pro"}, true},
		{"fs arguments without path", []string{"/fs home"}, true},
		{"fs absolute", []string{"/fs /home/"}, true},
		{"other command argument", []string{"/goal set read ~/Pro"}, true},
		{"second line", []string{"x", "/fs ~/a"}, true},
		{"cursor line out of range", nil, false},
	}
	for _, tt := range tests {
		line := len(tt.lines) - 1
		col := 0
		if line >= 0 {
			col = len(tt.lines[line])
		}
		if _, _, _, ok := findToken(tt.lines, line, col); ok != tt.ok {
			t.Errorf("%s: ok = %v, want %v", tt.name, ok, tt.ok)
		}
	}
}

func TestFsCompletionsAllowSpaces(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "My Projects", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "My Projects", "a b.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := fsCompletions(root, root+"/My Pro")
	if err != nil || len(items) != 1 || items[0].Value != root+"/My Projects/" {
		t.Fatalf("items %v err %v", items, err)
	}
	items, err = fsCompletions(root, root+"/My Projects/")
	if err != nil || len(items) != 2 || items[0].Value != root+"/My Projects/inner/" || items[1].Value != root+"/My Projects/a b.txt" {
		t.Fatalf("items %v err %v", items, err)
	}
}

func TestFsChoiceReplacesCommandWithPath(t *testing.T) {
	provider, err := newProvider(sdk.Context{}, &sdk.AutocompleteProvider{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ line, value, want string }{
		{"/fs ~/My", "~/My Projects/", `"~/My Projects/"`},
		{"/fs ~/Pro", "~/Projects/", "~/Projects/"},
		{"/fs ~/Pro", "~/Projects.txt", "~/Projects.txt"},
	}
	for _, tt := range tests {
		got, err := provider.ApplyCompletion(sdk.Context{}, []string{tt.line}, 0, len(tt.line), sdk.AutocompleteItem{Value: tt.value}, tt.line[len("/fs "):])
		if err != nil || got.Lines[0] != tt.want {
			t.Errorf("%q + %q = %q, %v; want %q", tt.line, tt.value, got.Lines, err, tt.want)
		}
	}
}
