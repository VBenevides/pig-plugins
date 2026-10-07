package lspext_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func write(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func home(t *testing.T) (*pigtest.Home, string, map[string]string) {
	t.Helper()
	pigtest.RequirePig(t)
	h := pigtest.NewHome(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	server, _ := filepath.Abs("../../testfixtures/lsp/server.py")
	ext, _ := filepath.Abs(".")
	write(t, filepath.Join(h.Work, "main.py"), "def alpha():\n    return ERROR_TOKEN\n\nalpha()\n")
	return h, ext, map[string]string{"ZED_PI_HARNESS_LSP_PYTHON": "[\"" + python + "\",\"" + server + "\"]", "FAKE_LSP_LOG": filepath.Join(h.Work, "server.log")}
}
func TestNativeNavigationStructureAndReadOnlyRename(t *testing.T) {
	h, ext, env := home(t)
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("lsp_diagnostics", map[string]any{"path": "main.py"})),
		pigtest.Calls(pigtest.Call("lsp_hover", map[string]any{"path": "main.py", "line": 3, "character": 2})),
		pigtest.Calls(pigtest.Call("lsp_definition", map[string]any{"path": "main.py", "line": 3, "character": 2})),
		pigtest.Calls(pigtest.Call("lsp_references", map[string]any{"path": "main.py", "line": 3, "character": 2, "includeDeclaration": true})),
		pigtest.Calls(pigtest.Call("lsp_symbols", map[string]any{"path": "main.py"})),
		pigtest.Calls(pigtest.Call("lsp_rename_preview", map[string]any{"path": "main.py", "line": 3, "character": 2, "new_name": "beta"})),
		pigtest.Calls(pigtest.Call("code_overview", map[string]any{"path": "main.py"})),
		pigtest.Calls(pigtest.Call("code_search", map[string]any{"path": "main.py", "kind": "function", "query": "alpha", "body": "ERROR_TOKEN", "lsp": false})),
		pigtest.Text("done"))
	defer mock.Close()
	result := h.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}, Env: env, Prompt: "Inspect alpha without applying a rename"})
	if result.ExitCode != 0 {
		t.Fatal(result)
	}
	results := pigtest.ToolResults(mock)
	wants := []string{"main.py:2:12 - error: fake: error_token found [F001]", "alpha", "main.py", "character", "alpha", "=> \"beta\"", "def alpha", "def alpha"}
	if len(results) != len(wants) {
		t.Fatal(results)
	}
	for i, want := range wants {
		if !strings.Contains(results[i], want) {
			t.Errorf("%d expected %q: %s", i, want, results[i])
		}
	}
	text, _ := os.ReadFile(filepath.Join(h.Work, "main.py"))
	if strings.Contains(string(text), "beta") {
		t.Fatal("rename changed disk")
	}
	log, _ := os.ReadFile(env["FAKE_LSP_LOG"])
	if !strings.Contains(string(log), `"applied": false`) {
		t.Fatal("server applyEdit was not refused", string(log))
	}
}
func TestPostEditDiagnosticsAppear(t *testing.T) {
	h, ext, env := home(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("write", map[string]any{"path": "main.py", "content": "def alpha():\n    return ERROR_TOKEN\n"})), pigtest.Text("done"))
	defer mock.Close()
	result := h.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}, Env: env, Prompt: "write the example"})
	if result.ExitCode != 0 {
		t.Fatal(result)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 1 || !strings.Contains(results[0], "error_token found") {
		t.Fatal(results)
	}
}
func TestHeadlessRejectsProjectExecutableConfiguration(t *testing.T) {
	h, ext, env := home(t)
	write(t, filepath.Join(h.Work, ".pi/lsp.json"), `{"servers":[{"id":"python","bin":"./server","include":["**/*.py"]}]}`)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("lsp_hover", map[string]any{"path": "main.py", "line": 0, "character": 5})), pigtest.Text("done"))
	defer mock.Close()
	h.Run(t, mock, pigtest.RunOptions{Extensions: []string{ext}, Env: env, Prompt: "inspect"})
	results := pigtest.ToolResults(mock)
	if len(results) != 1 || !strings.Contains(results[0], "project-local LSP config rejected") {
		t.Fatal(results)
	}
	if _, err := os.Stat(env["FAKE_LSP_LOG"]); !os.IsNotExist(err) {
		t.Fatal("untrusted config executed", err)
	}
}
