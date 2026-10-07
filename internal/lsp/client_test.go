package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func fake(t *testing.T, mode string) (*Client, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	server, _ := filepath.Abs("../../testfixtures/lsp/server.py")
	root := t.TempDir()
	file := filepath.Join(root, "main.py")
	put(t, file, "def alpha():\n    return ERROR_TOKEN\n\nalpha()\n")
	client, err := Start(context.Background(), Server{ID: "fake", Bin: python, Args: []string{server}, Env: map[string]string{"FAKE_LSP_MODE": mode}, DiagnosticsMS: 100}, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, file
}
func TestFreshDiagnosticsAndDiskSync(t *testing.T) {
	c, file := fake(t, "normal")
	uri, _, mark, changed, err := c.Sync(context.Background(), file, "python")
	if err != nil {
		t.Fatal(err)
	}
	items, published, err := c.Diagnostics(context.Background(), uri, mark, changed)
	if err != nil || !published || len(items) != 1 || items[0].Message != "error_token found" {
		t.Fatal(items, published, err)
	}
	put(t, file, "def alpha():\n    return 1\n\nalpha()\n")
	uri, _, mark, changed, err = c.Sync(context.Background(), file, "python")
	if err != nil {
		t.Fatal(err)
	}
	items, published, err = c.Diagnostics(context.Background(), uri, mark, changed)
	if err != nil || !published || len(items) != 0 {
		t.Fatal(items, published, err)
	}
}
func TestRenameRefusesApplyEdit(t *testing.T) {
	c, file := fake(t, "normal")
	uri, _, _, _, err := c.Sync(context.Background(), file, "python")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(file)
	raw, err := c.Request(context.Background(), "textDocument/rename", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": Position{Line: 3, Character: 2}, "newName": "beta"})
	if err != nil {
		t.Fatal(err)
	}
	var edit struct {
		Changes map[string][]struct {
			NewText string `json:"newText"`
		} `json:"changes"`
	}
	if err = json.Unmarshal(raw, &edit); err != nil {
		t.Fatal(err)
	}
	if len(edit.Changes[uri]) != 2 || edit.Changes[uri][0].NewText != "beta" {
		t.Fatal(string(raw))
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Fatal("rename changed disk")
	}
}
func TestCrashAndCancellationStopRequests(t *testing.T) {
	for _, mode := range []string{"hang", "crash"} {
		t.Run(mode, func(t *testing.T) {
			c, file := fake(t, mode)
			uri, _, _, _, err := c.Sync(context.Background(), file, "python")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, err = c.Request(ctx, "textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": Position{Line: 0, Character: 5}})
			if err == nil {
				t.Fatal("request succeeded despite server mode", mode)
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
}
func TestSilentDiagnosticsNotClean(t *testing.T) {
	c, file := fake(t, "silent")
	uri, _, mark, changed, err := c.Sync(context.Background(), file, "python")
	if err != nil {
		t.Fatal(err)
	}
	_, published, err := c.Diagnostics(context.Background(), uri, mark, changed)
	if err != nil || published {
		t.Fatal(published, err)
	}
}
func TestHeaderBoundBeforeNewline(t *testing.T) {
	reader, writer := io.Pipe()
	r := NewRPC(reader, nopWriteCloser{io.Discard})
	r.Start()
	go func() { defer writer.Close(); writer.Write([]byte(strings.Repeat("x", 8192))) }()
	select {
	case <-r.done:
		if !strings.Contains(r.failure.Error(), "buffer full") {
			t.Fatal(r.failure)
		}
	case <-time.After(time.Second):
		t.Fatal("header read was not bounded")
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
func TestUnknownDiagnosticURIsAreNotRetained(t *testing.T) {
	c, file := fake(t, "normal")
	for i := range 100 {
		raw, _ := json.Marshal(map[string]any{"uri": URI(filepath.Join(filepath.Dir(file), string(rune('a'+i))+".py")), "diagnostics": []any{map[string]any{"message": "x"}}})
		c.notify("textDocument/publishDiagnostics", raw)
	}
	if len(c.diagnostics) != 0 {
		t.Fatal("unsolicited diagnostics retained", len(c.diagnostics))
	}
}
func TestProjectCommandsRequireHashTrustAndCannotShadowDefaults(t *testing.T) {
	root := t.TempDir()
	agent := t.TempDir()
	put(t, filepath.Join(root, ".pi/lsp.json"), `{"version":1,"servers":[{"id":"local","bin":"./server","include":["**/*.py"]}]}`)
	env := func(key string) string {
		switch key {
		case "PIG_CODING_AGENT_DIR":
			return agent
		case "HOME":
			return agent
		case "PATH":
			return "/usr/bin"
		}
		return ""
	}
	if _, err := Load(root, env, nil); err == nil {
		t.Fatal("untrusted config accepted")
	}
	trusted, err := Load(root, env, func(string, string, []string) (bool, bool, error) { return true, true, nil })
	if err != nil || trusted[len(trusted)-1].ID != "local" {
		t.Fatal(trusted, err)
	}
	if _, err = Load(root, env, nil); err != nil {
		t.Fatal("saved trust not used", err)
	}
	put(t, filepath.Join(root, ".pi/lsp.json"), `{"version":1,"servers":[{"id":"local","bin":"./changed"}]}`)
	if _, err = Load(root, env, nil); err == nil {
		t.Fatal("changed hash accepted")
	}
	candidate := filepath.Join(root, "node_modules/.bin/gopls")
	put(t, candidate, "#!/bin/sh\nexit 9\n")
	os.Chmod(candidate, 0700)
	resolved := resolve(Server{Bin: "gopls"}, root, "", root, env)
	if resolved.Bin == candidate {
		t.Fatal("repository shadowing trusted server")
	}
}
