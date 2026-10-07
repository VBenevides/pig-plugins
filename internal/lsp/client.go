package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Position uses zero-based UTF-16 offsets, as the LSP wire protocol does.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
	Code     any    `json:"code"`
}
type document struct {
	text    string
	version int
}
type publication struct {
	items   []Diagnostic
	version int
	mark    uint64
}

// Client owns one server process and synchronized documents. It never applies server edits.
type Client struct {
	Spec         Server
	Root         string
	rpc          *RPC
	cmd          *exec.Cmd
	waited       chan struct{}
	mu           sync.Mutex
	syncMu       sync.Mutex
	docs         map[string]document
	diagnostics  map[string]publication
	updated      chan struct{}
	mark         uint64
	capabilities map[string]any
	closeOnce    sync.Once
}

func URI(file string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(file)}).String()
}
func FileURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Host != "" && u.Host != "localhost" {
		return "", errors.New("not a local file URI")
	}
	return filepath.FromSlash(u.Path), nil
}
func Start(ctx context.Context, spec Server, root string) (*Client, error) {
	cmd := exec.Command(spec.Bin, spec.Args...)
	cmd.Dir = spec.Cwd
	if cmd.Dir == "" {
		cmd.Dir = root
	}
	cmd.Env = os.Environ()
	for key, value := range spec.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, fmt.Errorf("start %s: %w", spec.ID, err)
	}
	c := &Client{Spec: spec, Root: root, cmd: cmd, waited: make(chan struct{}), docs: map[string]document{}, diagnostics: map[string]publication{}, updated: make(chan struct{}, 1)}
	c.rpc = NewRPC(out, in)
	c.rpc.Handle = c.handle
	c.rpc.Notify = c.notify
	c.rpc.Start()
	go func() { err := cmd.Wait(); c.rpc.Close(fmt.Errorf("%s exited: %v", spec.ID, err)); close(c.waited) }()
	params := map[string]any{"processId": os.Getpid(), "rootUri": URI(root), "workspaceFolders": []any{map[string]any{"name": filepath.Base(root), "uri": URI(root)}}, "initializationOptions": spec.InitializationOptions, "capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}, "workspace": map[string]any{"configuration": true, "workspaceFolders": true, "applyEdit": false}, "textDocument": map[string]any{"publishDiagnostics": map[string]any{"versionSupport": true}, "synchronization": map[string]any{"didSave": true}, "diagnostic": map[string]any{}, "documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true}}}}
	result, err := c.rpc.Request(ctx, "initialize", params, spec.startTimeout())
	if err != nil {
		c.Close()
		return nil, err
	}
	var init struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err = json.Unmarshal(result, &init); err != nil {
		c.Close()
		return nil, err
	}
	c.capabilities = init.Capabilities
	if encoding, ok := c.capabilities["positionEncoding"].(string); ok && encoding != "utf-16" {
		c.Close()
		return nil, fmt.Errorf("unsupported server position encoding %s", encoding)
	}
	if err = c.rpc.Send(ctx, "initialized", map[string]any{}); err == nil && spec.Settings != nil {
		err = c.rpc.Send(ctx, "workspace/didChangeConfiguration", map[string]any{"settings": spec.Settings})
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Client) handle(method string, p json.RawMessage) (any, error) {
	switch method {
	case "workspace/applyEdit":
		return map[string]any{"applied": false, "failureReason": "read-only LSP client"}, nil
	case "workspace/configuration":
		var params struct {
			Items []any `json:"items"`
		}
		json.Unmarshal(p, &params)
		out := make([]any, len(params.Items))
		for i := range out {
			out[i] = c.Spec.Settings
		}
		return out, nil
	case "workspace/workspaceFolders":
		return []any{map[string]any{"name": filepath.Base(c.Root), "uri": URI(c.Root)}}, nil
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported LSP server request %s", method)
}
func (c *Client) notify(method string, p json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var value struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
		Version     int          `json:"version"`
	}
	if json.Unmarshal(p, &value) != nil {
		return
	}
	c.mu.Lock()
	doc, exists := c.docs[value.URI]
	if !exists || value.Version != 0 && value.Version < doc.version {
		c.mu.Unlock()
		return
	}
	if len(p) > 1<<20 {
		c.mu.Unlock()
		c.rpc.Close(errors.New("LSP diagnostics exceed 1 MiB cache limit"))
		return
	}
	c.mark++
	c.diagnostics[value.URI] = publication{value.Diagnostics, value.Version, c.mark}
	c.mu.Unlock()
	select {
	case c.updated <- struct{}{}:
	default:
	}
}
func (c *Client) Alive() bool { return c.rpc.Alive() }
func (c *Client) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.rpc.Request(ctx, method, params, 30*time.Second)
}

// Save notifies servers that request didSave, without changing the disk document.
func (c *Client) Save(ctx context.Context, uri, text string) error {
	syncOptions, ok := c.capabilities["textDocumentSync"].(map[string]any)
	if !ok || syncOptions["save"] == nil || syncOptions["save"] == false {
		return nil
	}
	return c.rpc.Send(ctx, "textDocument/didSave", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "text": text,
	})
}

// Sync sends the current disk text and returns a mark for freshness checks.
func (c *Client) Sync(ctx context.Context, file, language string) (string, string, uint64, bool, error) {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	text, err := ReadText(file, c.Spec.fileLimit())
	if err != nil {
		return "", "", 0, false, err
	}
	uri := URI(file)
	c.mu.Lock()
	if _, exists := c.docs[uri]; !exists && len(c.docs) >= 32 {
		c.mu.Unlock()
		return "", "", 0, false, errors.New("LSP open-document limit (32)")
	}
	previous, exists := c.docs[uri]
	mark := c.mark
	if exists && previous.text == text {
		c.mu.Unlock()
		return uri, text, mark, false, nil
	}
	doc := document{text: text, version: previous.version + 1}
	c.docs[uri] = doc
	c.mu.Unlock()
	var method string
	var params any
	if !exists {
		method = "textDocument/didOpen"
		params = map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": language, "version": doc.version, "text": text}}
	} else {
		method = "textDocument/didChange"
		params = map[string]any{"textDocument": map[string]any{"uri": uri, "version": doc.version}, "contentChanges": []any{map[string]any{"text": text}}}
	}
	if err = c.rpc.Send(ctx, method, params); err != nil {
		return "", "", mark, true, err
	}
	return uri, text, mark, true, nil
}
func (c *Client) Diagnostics(ctx context.Context, uri string, mark uint64, fresh bool) ([]Diagnostic, bool, error) {
	if c.capabilities["diagnosticProvider"] != nil && c.capabilities["diagnosticProvider"] != false {
		raw, err := c.Request(ctx, "textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}})
		if err == nil {
			var result struct {
				Kind  string       `json:"kind"`
				Items []Diagnostic `json:"items"`
			}
			if err = json.Unmarshal(raw, &result); err != nil {
				return nil, false, err
			}
			if result.Kind == "full" {
				return result.Items, true, nil
			}
		} else if !strings.Contains(err.Error(), "-32601") {
			return nil, false, err
		}
	}
	timer := time.NewTimer(c.Spec.diagTimeout())
	defer timer.Stop()
	for {
		c.mu.Lock()
		p, exists := c.diagnostics[uri]
		c.mu.Unlock()
		if exists && (!fresh || p.mark > mark) {
			return p.items, true, nil
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-c.rpc.done:
			return nil, false, c.rpc.failure
		case <-timer.C:
			return nil, false, nil
		case <-c.updated:
		}
	}
}

// Close joins the process and kills its process group if polite shutdown fails.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = c.rpc.Request(ctx, "shutdown", nil, time.Second)
		_ = c.rpc.Send(ctx, "exit", nil)
		cancel()
		select {
		case <-c.waited:
		case <-time.After(time.Second):
		}
		// Descendants can remain even after the process-group leader exits.
		if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			fmt.Fprintln(os.Stderr, "LSP process-group TERM:", err)
		}
		if syscall.Kill(-c.cmd.Process.Pid, 0) == nil {
			time.Sleep(100 * time.Millisecond)
			if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
				fmt.Fprintln(os.Stderr, "LSP process-group KILL:", err)
			}
		}
		<-c.waited
		c.rpc.Close(nil)
	})
}

// ReadText rejects binary and oversized documents before synchronization.
func ReadText(file string, limit int64) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("LSP requires a regular file")
	}
	if info.Size() > limit {
		return "", fmt.Errorf("file exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", errors.New("file exceeds byte limit")
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", errors.New("binary or invalid UTF-8 document")
	}
	return string(data), nil
}
