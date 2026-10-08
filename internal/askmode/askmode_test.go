package askmode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyBash(t *testing.T) {
	ok := []string{"ls -la", "git status && git diff HEAD", "cat a | grep x | wc -l", "find . -name '*.go'", "rg foo src"}
	bad := []string{"rm -rf x", "echo hi > f", "ls; touch x", "cat a >> b", "find . -delete", "find . -exec rm {} +",
		"git commit -m x", "git -C /tmp status", "sort -o out in", "echo $(rm x)", "ls &", "sed -i s/a/b/ f", "git diff --output=x", "", "ls\nrm x"}
	for _, c := range ok {
		if got, why := ReadOnlyBash(c); !got {
			t.Errorf("%q rejected: %s", c, why)
		}
	}
	for _, c := range bad {
		if got, _ := ReadOnlyBash(c); got {
			t.Errorf("%q accepted", c)
		}
	}
}

func TestInAgentWork(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".agent-work"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cwd, ".agent-work", "link")); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		".agent-work/notes/new.md":             true,
		filepath.Join(cwd, ".agent-work", "x"): true,
		"main.go":                              false,
		".agent-work/../main.go":               false,
		".agent-work/link/x":                   false,
		".agent-work":                          false,
		"../other/.agent-work/x":               false,
	}
	for path, want := range cases {
		got, err := InAgentWork(cwd, path)
		if err != nil || got != want {
			t.Errorf("%s: got %v, %v want %v", path, got, err, want)
		}
	}
}

func TestReadOnlyTool(t *testing.T) {
	for name, want := range map[string]bool{"read": true, "lsp_hover": true, "memory_search": true, "write": false, "edit": false, "bash": false, "mcp_deploy": false} {
		if ReadOnlyTool(name) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}
