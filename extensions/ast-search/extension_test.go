package astsearchext

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestArgumentValidation(t *testing.T) {
	for _, params := range []map[string]any{
		{}, {"pattern": nil, "lang": "go"}, {"pattern": "x", "lang": nil}, {"pattern": "x", "lang": "go", "path": false},
		{"pattern": "x", "lang": "go", "limit": 1.5}, {"pattern": "x", "lang": "go", "limit": 201.0},
		{"pattern": "x", "lang": "go", "rewrite": "danger"}, {"pattern": "x", "lang": "go", "limit": nil},
	} {
		if _, err := options(params); err == nil {
			t.Fatalf("accepted: %+v", params)
		}
	}
	got, err := options(map[string]any{"pattern": "x", "lang": "go", "limit": 5.0, "path": "."})
	if err != nil || got.Limit != 5 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSearchReadEditInNativeHost(t *testing.T) {
	pigtest.RequirePig(t)
	binary := os.Getenv("PI_TEST_AST_GREP")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("ast-grep")
		if err != nil {
			t.Skip("optional ast-grep unavailable")
		}
	}
	extension, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	hashlineExt, err := filepath.Abs("../hashline-edit")
	if err != nil {
		t.Fatal(err)
	}
	home := pigtest.NewHome(t)
	source := "package example\nfunc Example() { println(1) }\n"
	path := filepath.Join(home.Work, "example.go")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	edit := map[string]any{"path": "example.go", "version": hashline.ContentVersion([]byte(source)), "edits": []any{map[string]any{"op": "replace", "anchor": hashline.FormatAnchor(2, "func Example() { println(1) }"), "lines": []any{"func Example() { println(2) }"}}}}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("ast_search", map[string]any{"pattern": "println($A)", "lang": "go"})),
		pigtest.Calls(pigtest.Call("edit", edit)),
		pigtest.Calls(pigtest.Call("read", map[string]any{"path": "example.go", "offset": 2, "limit": 1})),
		pigtest.Calls(pigtest.Call("edit", edit)), pigtest.Text("done"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extension, hashlineExt}, Env: map[string]string{"PATH": filepath.Dir(binary) + string(os.PathListSeparator) + os.Getenv("PATH")}})
	if result.ExitCode != 0 {
		t.Fatalf("%d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 4 || !strings.Contains(results[0], "AST search: 1 match") || !strings.Contains(results[0], "Discovery only") || !strings.Contains(results[1], "edit rejected") || !strings.Contains(results[2], "Version:") || !strings.Contains(results[3], "Applied 1 edit") {
		t.Fatalf("%q", results)
	}
	names := pigtest.ToolNames(mock)
	if !reflect.DeepEqual(names, []string{"read", "bash", "edit", "write", "ast_search"}) {
		t.Fatal(names)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "package example\nfunc Example() { println(2) }\n" {
		t.Fatalf("%q %v", got, err)
	}
}
