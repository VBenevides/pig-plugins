// Package lspext exposes read-only language-server and structural code tools to PiG.
package lspext

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/lsp"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

const Name = "lsp"

type extension struct {
	mu        sync.Mutex
	manager   *lsp.Manager
	trustMu   sync.Mutex
	decisions map[string]bool
}

func (x *extension) get() *lsp.Manager {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.manager == nil {
		x.manager = lsp.NewManager(os.Getenv)
	}
	return x.manager
}
func (x *extension) trust(ctx sdk.Context) lsp.Trust {
	return func(path, hash string, binaries []string) (bool, bool, error) {
		x.trustMu.Lock()
		defer x.trustMu.Unlock()
		if trusted, ok := x.decisions[hash]; ok {
			return trusted, false, nil
		}
		if !ctx.HasUI() {
			return false, false, nil
		}
		choice, _, err := ctx.Select("Project-local lsp config wants to auto-run binaries.\n\nConfig: "+path+"\nHash: "+hash+"\nBinaries:\n  - "+strings.Join(binaries, "\n  - ")+"\n\nTrust this config?", []string{"Trust once", "Trust always", "Reject"})
		if err != nil {
			return false, false, err
		}
		trusted := choice == "Trust once" || choice == "Trust always"
		x.decisions[hash] = trusted
		return trusted, choice == "Trust always", nil
	}
}
func object(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func num(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}
func pathProps() map[string]any { return map[string]any{"path": str("File path")} }
func positionProps() map[string]any {
	p := pathProps()
	p["line"] = num("Zero-based line number")
	p["character"] = num("Zero-based UTF-16 character offset")
	return p
}

// Extension returns the native PiG factory. The upstream core position contract is zero-based UTF-16.
func Extension() *sdk.Extension {
	x := &extension{decisions: map[string]bool{}}
	e := sdk.New(Name)
	register := func(name, label, description, snippet string, params map[string]any) {
		e.RegisterTool(sdk.ToolDefinition{Name: name, Label: label, Description: description, PromptSnippet: snippet, Exposure: sdk.ToolExposureDeferred, Parameters: params, Execute: func(ctx sdk.Context, p map[string]any) (any, error) { return x.execute(ctx, name, p) }})
	}
	register("lsp_diagnostics", "LSP Diagnostics", "Return latest LSP diagnostics collected for a file.", "Inspect latest LSP diagnostics for a file", object(pathProps(), "path"))
	register("lsp_hover", "LSP Hover", "Request hover information from the matching LSP server.", "Request hover information at a file position", object(positionProps(), "path", "line", "character"))
	register("lsp_definition", "LSP Definition", "Request definition locations from the matching LSP server.", "Find definition locations for a symbol at a file position", object(positionProps(), "path", "line", "character"))
	ref := positionProps()
	ref["includeDeclaration"] = map[string]any{"type": "boolean", "default": false, "description": "Include the declaration in references"}
	register("lsp_references", "LSP References", "Request references from the matching LSP server.", "Find references for a symbol at a file position", object(ref, "path", "line", "character"))
	register("lsp_symbols", "LSP Symbols", "Request document symbols from the matching LSP server.", "List document symbols for a file using LSP", object(pathProps(), "path"))
	rename := positionProps()
	rename["new_name"] = str("New identifier (preview only)")
	rename["limit"] = num("Maximum edits shown (default 50)")
	register("lsp_rename_preview", "lsp_rename_preview", "Preview a language-server rename without applying any edit.", "Preview symbol rename", object(rename, "path", "line", "character", "new_name"))
	register("code_overview", "code_overview", "Heuristic source outline or directory overview. Not AST matching. Read-only.", "Inspect code structure", object(map[string]any{"path": str("File or directory, default working directory"), "depth": num("Directory depth, default 2"), "max_files": num("Maximum files, default 60")}))
	register("code_search", "code_search", "Heuristic structural search by declaration name, kind and body. Not AST matching. Optionally adds LSP workspace symbols. Read-only.", "Search declarations and bodies", object(map[string]any{"path": str("File or directory, default working directory"), "query": str("Case-insensitive declaration name filter"), "kind": map[string]any{"type": "string", "enum": lsp.Kinds}, "body": str("Body regular expression"), "regex": map[string]any{"type": "boolean"}, "lsp": map[string]any{"type": "boolean"}, "limit": num("Maximum matches, default 50")}))
	e.OnEvent("session_shutdown", func(_ sdk.Context, _ map[string]any) (any, error) {
		x.mu.Lock()
		manager := x.manager
		x.manager = nil
		x.mu.Unlock()
		if manager != nil {
			manager.Close()
		}
		return nil, nil
	})
	e.OnEvent("session_start", func(ctx sdk.Context, _ map[string]any) (any, error) {
		active, err := ctx.GetActiveTools()
		if err != nil {
			return nil, err
		}
		if !slices.Contains(active, "tool_search") && !slices.Contains(active, "codemode") {
			active = append(active, "lsp_diagnostics", "lsp_hover", "lsp_definition", "lsp_references", "lsp_symbols", "lsp_rename_preview", "code_overview", "code_search")
			ctx.SetActiveTools(active)
		}
		return nil, nil
	})
	e.OnEvent("tool_result", x.afterEdit)
	return e
}
func integer(p map[string]any, key string, fallback, minValue, maxValue int) (int, error) {
	value, ok := p[key]
	if !ok {
		return fallback, nil
	}
	n, ok := value.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < float64(minValue) || n > float64(maxValue) {
		return 0, fmt.Errorf("%s must be an integer in %d..%d", key, minValue, maxValue)
	}
	return int(n), nil
}
func required(p map[string]any, key string) (string, error) {
	s, ok := p[key].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return s, nil
}
func result(text string, details any) sdk.ToolResult {
	if len(text) > 12000 {
		text = text[:12000] + "\n… output truncated"
	}
	return sdk.ToolResult{Content: text, Details: details}
}
func pretty(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "No result"
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	if s, ok := value.(string); ok {
		return s
	}
	data, _ := json.MarshalIndent(value, "", "  ")
	return string(data)
}
func position(p map[string]any, text string) (lsp.Position, error) {
	line, err := integer(p, "line", -1, 0, 1<<30)
	if err != nil {
		return lsp.Position{}, err
	}
	character, err := integer(p, "character", -1, 0, 1<<30)
	if err != nil {
		return lsp.Position{}, err
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if line < 0 || line >= len(lines) {
		return lsp.Position{}, fmt.Errorf("line is outside the file")
	}
	if character < 0 || character > len(utf16.Encode([]rune(lines[line]))) {
		return lsp.Position{}, fmt.Errorf("character is outside the line")
	}
	return lsp.Position{Line: line, Character: character}, nil
}
func (x *extension) execute(ctx sdk.Context, name string, p map[string]any) (any, error) {
	run, cancel := sdkctx.Request(ctx)
	defer cancel()
	run, deadline := context.WithTimeout(run, 60*time.Second)
	defer deadline()
	if name == "code_overview" || name == "code_search" {
		return x.structure(ctx, run, name, p)
	}
	path, err := required(p, "path")
	if err != nil {
		return nil, err
	}
	client, uri, text, mark, changed, err := x.get().Open(run, path, ctx.Cwd(), x.trust(ctx))
	if err != nil {
		return nil, err
	}
	if name == "lsp_diagnostics" {
		items, published, err := client.Diagnostics(run, uri, mark, changed)
		if err != nil {
			return nil, err
		}
		if !published {
			return result("LSP diagnostics: server sent no diagnostics within the wait limit. This is not a clean result.", map[string]any{"diagnostics": nil}), nil
		}
		file, _ := lsp.FileURI(uri)
		return result("LSP diagnostics:\n\n"+formatDiagnostics(client.Spec.ID, file, client.Root, items), map[string]any{"count": len(items)}), nil
	}
	params := map[string]any{"textDocument": map[string]any{"uri": uri}}
	method := "textDocument/documentSymbol"
	if name != "lsp_symbols" {
		pos, err := position(p, text)
		if err != nil {
			return nil, err
		}
		params["position"] = pos
		switch name {
		case "lsp_hover":
			method = "textDocument/hover"
		case "lsp_definition":
			method = "textDocument/definition"
		case "lsp_references":
			method = "textDocument/references"
			include, _ := p["includeDeclaration"].(bool)
			params["context"] = map[string]any{"includeDeclaration": include}
		case "lsp_rename_preview":
			method = "textDocument/rename"
			newName, err := required(p, "new_name")
			if err != nil {
				return nil, err
			}
			params["newName"] = newName
		}
	}
	raw, err := client.Request(run, method, params)
	if err != nil {
		return nil, err
	}
	if name == "lsp_rename_preview" {
		limit, err := integer(p, "limit", 50, 1, 200)
		if err != nil {
			return nil, err
		}
		preview, err := renamePreview(raw, ctx.Cwd(), limit)
		if err != nil {
			return nil, err
		}
		return result(preview, map[string]any{"preview": true, "applied": false}), nil
	}
	return result(pretty(raw), map[string]any{}), nil
}
func formatDiagnostics(server, file, root string, items []lsp.Diagnostic) string {
	if len(items) == 0 {
		return "✅ " + server + ": no diagnostics"
	}
	rel, _ := filepath.Rel(root, file)
	rows := []string{"⚠️ " + server + ":"}
	labels := map[int]string{1: "error", 2: "warning", 3: "info", 4: "hint"}
	for _, d := range items[:min(20, len(items))] {
		label := labels[d.Severity]
		if label == "" {
			label = "diagnostic"
		}
		source := ""
		if d.Source != "" {
			source = d.Source + ": "
		}
		code := ""
		if d.Code != nil {
			code = fmt.Sprintf(" [%v]", d.Code)
		}
		rows = append(rows, fmt.Sprintf("%s:%d:%d - %s: %s%s%s", rel, d.Range.Start.Line+1, d.Range.Start.Character+1, label, source, d.Message, code))
	}
	if len(items) > 20 {
		rows = append(rows, fmt.Sprintf("… %d more diagnostics", len(items)-20))
	}
	return strings.Join(rows, "\n")
}
func (x *extension) afterEdit(ctx sdk.Context, data map[string]any) (any, error) {
	name, _ := data["toolName"].(string)
	if name != "write" && name != "edit" || data["isError"] == true {
		return nil, nil
	}
	input, _ := data["input"].(map[string]any)
	path, _ := input["path"].(string)
	if path == "" {
		return nil, nil
	}
	run, cancel := sdkctx.Request(ctx)
	defer cancel()
	run, timeout := context.WithTimeout(run, 6*time.Second)
	defer timeout()
	opened, err := x.get().OpenAll(run, path, ctx.Cwd(), x.trust(ctx))
	var rows []string
	if err != nil {
		rows = append(rows, "⚠️ "+err.Error())
	}
	for _, o := range opened {
		if o.Err != nil {
			rows = append(rows, "⚠️ "+o.Err.Error())
			continue
		}
		if err := o.Client.Save(run, o.URI, o.Text); err != nil {
			rows = append(rows, "⚠️ "+err.Error())
			continue
		}
		items, published, err := o.Client.Diagnostics(run, o.URI, o.Mark, o.Changed)
		if err != nil {
			rows = append(rows, "⚠️ "+err.Error())
		} else if published {
			file, _ := lsp.FileURI(o.URI)
			rows = append(rows, formatDiagnostics(o.Client.Spec.ID, file, o.Client.Root, items))
		} else {
			rows = append(rows, o.Client.Spec.ID+": analysis did not complete within the wait limit.")
		}
	}
	summary := "LSP diagnostics:\n\n" + strings.Join(rows, "\n")
	content, _ := data["content"].([]any)
	content = append(slices.Clone(content), map[string]any{"type": "text", "text": summary})
	return map[string]any{"content": content}, nil
}
