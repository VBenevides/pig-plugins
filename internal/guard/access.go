package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/hashline"
)

// Only reviewed local read tools enter this policy. Other tools retain their
// own admission rules; prefixes and discovery do not imply read-only behavior.
var readTools = map[string]bool{
	"read": true, "grep": true, "find": true, "ls": true,
	"ast_search": true, "code_overview": true, "code_search": true,
	"lsp_diagnostics": true, "lsp_hover": true, "lsp_definition": true,
	"lsp_references": true, "lsp_symbols": true, "lsp_rename_preview": true,
}

// CheckAccess uses an immutable per-admission policy snapshot. The legacy
// approval modes remain usable by migration/tests, never by the new runtime.
func (g *Gate) CheckAccess(ctx context.Context, call Call, mode AccessMode) Decision {
	if _, ok := ParseAccessMode(string(mode)); !ok {
		return block("invalid access mode %q", mode)
	}
	g.mu.RLock()
	local := NewGate(Settings{Mode: Interactive, Lancet: g.lancetOn, Disabled: mode == ModeOff}, g.scorer)
	local.access, local.allowlist = mode, g.allowlist
	g.mu.RUnlock()
	return local.Check(ctx, call)
}

// readScope identifies a local resource, not an authorization to traverse an
// arbitrary directory tree. Recursive searches and workspace-wide LSP requests
// have unknown scope and need consent in strict mode rather than a costly walk.
func (g *Gate) readScope(call Call) (string, string) {
	raw, ok := call.Input["path"].(string)
	if !ok && call.Input["path"] != nil {
		return "", "invalid read path"
	}
	if raw == "" {
		switch call.Tool {
		case "read", "lsp_diagnostics", "lsp_hover", "lsp_definition", "lsp_references", "lsp_symbols", "lsp_rename_preview":
			return "", "missing read path"
		}
		raw = "."
	}
	path := hashline.ResolveToolPath(raw, call.Cwd)
	real := RealTarget(path)
	if real == "" {
		return path, "unknown read target (cannot resolve path)"
	}
	if g.matcher.IsProtected(path) || g.matcher.IsProtected(real) {
		return path, "protected file may contain credentials or secrets"
	}
	switch call.Tool {
	case "lsp_definition", "lsp_references", "lsp_rename_preview":
		return path, "language-server request can inspect other workspace files; scope is unknown"
	}
	info, err := os.Stat(real)
	if err != nil {
		return path, "read target cannot be inspected"
	}
	if info.IsDir() && call.Tool != "ls" {
		return path, "directory read/search can include protected descendants; scope is unknown"
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return path, "special-file read has unknown scope"
	}
	return path, ""
}

func (g *Gate) checkRead(call Call) (Decision, error) {
	path, risk := g.readScope(call)
	if risk == "" {
		return allow, nil
	}
	if why := g.cannotAsk(call); why != "" {
		return block("blocked %s read: %s; %s", call.Tool, risk, why), nil
	}
	if path == "" {
		return block("blocked %s: %s", call.Tool, risk), nil
	}
	body := func() string {
		return fmt.Sprintf("Risk Description:\n- %s\n\nAffected items:\n- read scope: %s - disclose contents or metadata\n\nAllow this read?", risk, path)
	}
	// Include exact tool arguments: a narrow read grant must not authorize a
	// recursive search, a different query or a workspace-wide LSP request.
	grant, err := readGrant(call, path)
	if err != nil {
		return Decision{}, err
	}
	if !g.ask(call, "Read requires approval: "+call.Tool, body, grant) {
		return block("user denied %s read of %s", call.Tool, path), nil
	}
	return allow, nil
}

// shellReadRisk is intentionally conservative. Only simple literal file reads
// have known scope; lists, pipelines, globs and option-bearing forms are not
// guessed. They can still be approved explicitly in strict mode.
func (g *Gate) shellReadRisk(command, cwd string) string {
	if strings.ContainsAny(command, "|;&\n*?[") {
		return "shell read scope is unknown"
	}
	words := roughTokenize(command)
	if len(words) == 0 {
		return "shell read scope is unknown"
	}
	args := words[1:]
	if words[0] == "grep" || words[0] == "rg" {
		if len(args) == 0 {
			return "shell search scope is unknown"
		}
		args = args[1:]
	}
	if len(args) == 0 {
		if words[0] == "pwd" || words[0] == "ls" {
			args = []string{"."}
		} else {
			return "shell read scope is unknown"
		}
	}
	for _, raw := range args {
		if strings.HasPrefix(raw, "-") {
			return "shell read options have unknown scope"
		}
		// Do not misinterpret concatenated quoted words as a resolved filename.
		raw = strings.Trim(raw, "\"'")
		if strings.ContainsAny(raw, "\"'") {
			return "shell read scope is unknown"
		}
		tool := "read"
		if words[0] == "ls" || words[0] == "pwd" || words[0] == "stat" || words[0] == "readlink" {
			tool = "ls"
		}
		_, risk := g.readScope(Call{Tool: tool, Input: map[string]any{"path": raw}, Cwd: cwd})
		if risk != "" {
			return risk
		}
	}
	return ""
}

func readGrant(call Call, path string) (Grant, error) {
	data, err := json.Marshal(call.Input)
	if err != nil {
		return Grant{}, fmt.Errorf("encode read grant: %w", err)
	}
	return Grant{Tool: call.Tool, Command: string(data), Item: path}, nil
}
