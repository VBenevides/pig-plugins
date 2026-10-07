package lsp

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStructureKindsBodiesAndIgnoreRules(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "go.mod"), "module fixture\n")
	put(t, filepath.Join(root, "main.go"), "package fixture\n\nimport \"fmt\"\n\nfunc Alpha() { fmt.Println(\"marker\") }\n\nfunc Beta() { }\n")
	put(t, filepath.Join(root, "hidden.go"), "package fixture\nfunc Hidden() { }\n")
	put(t, filepath.Join(root, ".gitignore"), "hidden.go\n")
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	found, err := SearchStructure(context.Background(), root, SearchOptions{Kind: "function", Name: "(?<=Al)pha", Regex: true, Body: "(?=marker)marker", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if found.Total != 1 || len(found.Matches) != 1 || found.Matches[0].Name != "Alpha" {
		t.Fatal(found)
	}
	found, err = SearchStructure(context.Background(), root, SearchOptions{Kind: "import", Limit: 50})
	if err != nil || found.Total != 1 || found.Matches[0].Kind != "import" {
		t.Fatal(found, err)
	}
	found, err = SearchStructure(context.Background(), root, SearchOptions{Kind: "any", Name: "Hidden", Limit: 50})
	if err != nil || found.Total != 0 {
		t.Fatal(found, err)
	}
}
