package lspext

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/lsp"
)

func (x *extension) structure(ctx sdk.Context, run context.Context, name string, p map[string]any) (any, error) {
	path, _ := p["path"].(string)
	if path == "" {
		path = ctx.Cwd()
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(ctx.Cwd(), path)
	}
	if name == "code_overview" {
		depth, err := integer(p, "depth", 2, 0, 10)
		if err != nil {
			return nil, err
		}
		maxFiles, err := integer(p, "max_files", 60, 1, 200)
		if err != nil {
			return nil, err
		}
		text, err := lsp.Overview(run, path, ctx.Cwd(), depth, maxFiles)
		if err != nil {
			return nil, err
		}
		return result("Heuristic outline (not AST matching):\n"+text, map[string]any{}), nil
	}
	query, _ := p["query"].(string)
	kind, _ := p["kind"].(string)
	if kind == "" {
		kind = "any"
	}
	if !slices.Contains(lsp.Kinds, kind) {
		return nil, fmt.Errorf("invalid kind %s", kind)
	}
	body, _ := p["body"].(string)
	regex, _ := p["regex"].(bool)
	limit, err := integer(p, "limit", 50, 1, 200)
	if err != nil {
		return nil, err
	}
	if query == "" && kind == "any" && body == "" {
		return nil, fmt.Errorf("give query, kind or body")
	}
	found, err := lsp.SearchStructure(run, path, lsp.SearchOptions{Name: query, Kind: kind, Body: body, Regex: regex, Limit: limit})
	if err != nil {
		return nil, err
	}
	text := fmt.Sprintf("Heuristic structural matches (not AST matching): %d across %d file(s), %d skipped.\n%s", found.Total, found.Scanned, found.Skipped, lsp.FormatMatches(found, ctx.Cwd()))
	if found.Total > len(found.Matches) {
		text += fmt.Sprintf("\n… %d more matches", found.Total-len(found.Matches))
	}
	if found.Capped {
		text += "\nOnly the first 3000 source files were considered."
	}
	lspCount := 0
	if query != "" && p["lsp"] != false {
		if len(found.Files) > 0 {
			client, _, _, _, _, err := x.get().Open(run, found.Files[0], ctx.Cwd(), x.trust(ctx))
			if err == nil {
				raw, e := client.Request(run, "workspace/symbol", map[string]any{"query": query})
				if e == nil {
					var items []json.RawMessage
					json.Unmarshal(raw, &items)
					lspCount = len(items)
					text += "\n\nLSP workspace symbols:\n" + pretty(raw)
				} else {
					text += "\n\nLSP workspace symbols unavailable: " + e.Error()
				}
			} else {
				text += "\n\nLSP workspace symbols unavailable: " + err.Error()
			}
		}
	}
	return result(text, map[string]any{"structural": found.Total, "lsp": lspCount}), nil
}

type textEdit struct {
	Range   lsp.Range `json:"range"`
	NewText string    `json:"newText"`
}

func renamePreview(raw json.RawMessage, cwd string, limit int) (string, error) {
	var edit struct {
		Changes         map[string][]textEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits  []textEdit `json:"edits"`
			Kind   string     `json:"kind"`
			URI    string     `json:"uri"`
			OldURI string     `json:"oldUri"`
			NewURI string     `json:"newUri"`
		} `json:"documentChanges"`
	}
	if err := json.Unmarshal(raw, &edit); err != nil {
		return "", err
	}
	rows := []string{"Rename preview only. No files changed."}
	count := 0
	add := func(uri string, edits []textEdit) {
		file, err := lsp.FileURI(uri)
		if err != nil {
			rows = append(rows, "Non-local URI: "+uri)
			return
		}
		rel, _ := filepath.Rel(cwd, file)
		for _, e := range edits {
			count++
			if count > limit {
				continue
			}
			rows = append(rows, fmt.Sprintf("%s:%d:%d-%d:%d => %q", rel, e.Range.Start.Line+1, e.Range.Start.Character+1, e.Range.End.Line+1, e.Range.End.Character+1, e.NewText))
		}
	}
	var keys []string
	for uri := range edit.Changes {
		keys = append(keys, uri)
	}
	slices.Sort(keys)
	for _, uri := range keys {
		add(uri, edit.Changes[uri])
	}
	for _, d := range edit.DocumentChanges {
		if d.Kind != "" {
			count++
			if count <= limit {
				rows = append(rows, strings.TrimSpace(d.Kind+" "+d.URI+" "+d.OldURI+" -> "+d.NewURI))
			}
		} else {
			add(d.TextDocument.URI, d.Edits)
		}
	}
	if count == 0 {
		rows = append(rows, "No edits returned.")
	} else if count > limit {
		rows = append(rows, fmt.Sprintf("… %d more edits", count-limit))
	}
	return strings.Join(rows, "\n"), nil
}
