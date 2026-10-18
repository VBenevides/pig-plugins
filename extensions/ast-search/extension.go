// Package astsearchext adds discovery-only AST-pattern matching to PiG.
package astsearchext

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/astsearch"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

func Extension() *sdk.Extension {
	e := sdk.New("ast-search")
	e.RegisterTool(sdk.ToolDefinition{
		Name: "ast_search", Label: "ast_search", Exposure: sdk.ToolExposureDeferred,
		Description:   "Read-only AST-pattern search using optional ast-grep, with no version restriction. Returns locations and bounded snippets, not edit authority. Read matching ranges before editing. Uses code_search source-file discovery; files over 1 MiB are skipped. No parser-backed maps or rewrites.",
		PromptSnippet: "Find structural AST-pattern matches; read the source before editing",
		Parameters: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"pattern", "lang"}, "properties": map[string]any{
			"pattern": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192, "description": "AST pattern, for example console.log($$$ARGS)"},
			"lang":    map[string]any{"type": "string", "minLength": 1, "maxLength": 32, "description": "Built-in ast-grep language, for example go or typescript"},
			"path":    map[string]any{"type": "string", "description": "Source file or directory; default working directory"},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": astsearch.MaxResults, "description": "Maximum matches (default 50)"},
		}}, Execute: execute,
	})
	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		tools, err := ctx.GetActiveTools()
		if err != nil {
			return nil, err
		}
		if !slices.Contains(tools, "tool_search") && !slices.Contains(tools, "codemode") && !slices.Contains(tools, "ast_search") {
			ctx.SetActiveTools(append(tools, "ast_search"))
		}
		return nil, nil
	})
	return e
}

func options(params map[string]any) (astsearch.Options, error) {
	var out astsearch.Options
	for key, value := range params {
		switch key {
		case "pattern", "lang", "path":
			text, ok := value.(string)
			if !ok {
				return out, fmt.Errorf("%s must be a string", key)
			}
			switch key {
			case "pattern":
				out.Pattern = text
			case "lang":
				out.Language = text
			case "path":
				out.Path = text
			}
		case "limit":
			n, ok := value.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < 1 || n > astsearch.MaxResults {
				return out, fmt.Errorf("limit must be an integer in 1..%d", astsearch.MaxResults)
			}
			out.Limit = int(n)
		default:
			return out, fmt.Errorf("unknown argument %q", key)
		}
	}
	if strings.TrimSpace(out.Pattern) == "" || strings.TrimSpace(out.Language) == "" {
		return out, errors.New("pattern and lang are required nonempty strings")
	}
	return out, nil
}

func execute(ctx sdk.Context, params map[string]any) (any, error) {
	opts, err := options(params)
	if err != nil {
		return sdk.ToolResult{Content: err.Error(), IsError: true, Details: map[string]any{"code": "invalid_arguments"}}, nil
	}
	request, cancel := sdkctx.Request(ctx)
	defer cancel()
	result, err := (astsearch.Searcher{}).Search(request, ctx.Cwd(), opts)
	if err != nil {
		code := "search_failed"
		var searchErr *astsearch.Error
		if errors.As(err, &searchErr) {
			code = searchErr.Code
		}
		return sdk.ToolResult{Content: err.Error(), IsError: true, Details: map[string]any{"code": code}}, nil
	}
	var text strings.Builder
	fmt.Fprintf(&text, "AST search: %d match(es). Locations are 1-based; end is exclusive. Discovery only: read source ranges before editing.\n", len(result.Matches))
	for _, match := range result.Matches {
		fmt.Fprintf(&text, "%s:%d:%d: %q", match.File, match.Start.Line, match.Start.Column, match.Text)
		if match.SnippetTruncated {
			text.WriteString(" [snippet shortened]")
		}
		text.WriteByte('\n')
	}
	if result.Truncated {
		text.WriteString("Results truncated by discovery, capture, match, or output limits; narrow the path or pattern.\n")
	}
	if result.Skipped > 0 {
		fmt.Fprintf(&text, "Skipped %d oversized/nonregular/unavailable files.\n", result.Skipped)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(&text, "Warning: %s\n", warning)
	}
	// Keep typed result records in details without any hashline Engine snapshot.
	// No result from this tool can authorize a mutation.
	details := map[string]any{"matches": result.Matches, "truncated": result.Truncated, "skipped": result.Skipped, "warnings": result.Warnings}
	output := sdk.ToolResult{Content: text.String(), Details: details}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	if len(data) > 50<<10 {
		return sdk.ToolResult{Content: "AST search output exceeds the 50 KiB response budget; narrow the path or pattern.", IsError: true, Details: map[string]any{"code": "output_limit"}}, nil
	}
	return output, nil
}
