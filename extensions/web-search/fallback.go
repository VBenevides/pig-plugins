package websearchext

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func registerSearchServers(e *sdk.Extension) {
	for _, server := range []struct{ name, url, keyEnv, header, prefix string }{
		{"parallel", "https://search.parallel.ai/mcp", "PARALLEL_API_KEY", "Authorization", "Bearer "},
		{"exa", "https://mcp.exa.ai/mcp", "EXA_API_KEY", "x-api-key", ""},
	} {
		config := sdk.McpServerConfig{URL: server.url, Exposure: sdk.McpExposureDeferred}
		timeout := 30.0
		config.Timeout = &timeout
		if os.Getenv(server.keyEnv) != "" {
			config.Headers = sdk.NewOrderedStrings(server.header, server.prefix+"${"+server.keyEnv+"}")
		}
		if err := e.RegisterMcpServer(server.name, config); err != nil {
			panic(fmt.Errorf("register %s search MCP: %w", server.name, err))
		}
	}
}

type searchAttempt struct {
	provider string
	run      func() sdk.ToolResult
}

// runSearchChain records failed attempts even when a later provider succeeds.
// A cancellation or permission refusal must never trigger another provider.
func runSearchChain(cancelled func() bool, attempts []searchAttempt) sdk.ToolResult {
	failures := []map[string]any{}
	for _, attempt := range attempts {
		if cancelled() {
			return fail(errors.New("web search cancelled"), map[string]any{"attempts": failures})
		}
		result := attempt.run()
		details, _ := result.Details.(map[string]any)
		if details == nil {
			details = map[string]any{"nativeDetails": result.Details}
			result.Details = details
		}
		details["searchProvider"] = attempt.provider
		details["attempts"] = failures
		if cancelled() {
			return fail(errors.New("web search cancelled"), map[string]any{"attempts": failures, "searchProvider": attempt.provider})
		}
		if !result.IsError {
			return result
		}
		failures = append(failures, map[string]any{"provider": attempt.provider, "error": result.Content})
		details["attempts"] = failures
		if details["stopFallback"] == true || cancelled() {
			return result
		}
	}
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reasons = append(reasons, fmt.Sprintf("%s: %s", failure["provider"], failure["error"]))
	}
	return fail(fmt.Errorf("all web search providers failed: %s", strings.Join(reasons, "; ")), map[string]any{"attempts": failures})
}

func searchWithFallback(ctx sdk.Context, query string, urls []string) sdk.ToolResult {
	objective := query
	if len(urls) != 0 {
		objective += "\nAnalyze these URLs: " + strings.Join(urls, ", ")
	}
	return runSearchChain(func() bool { return ctx.Err() != nil }, []searchAttempt{
		{"parallel-mcp", func() sdk.ToolResult {
			return mcpSearch(ctx, "mcp__parallel__web_search", map[string]any{"objective": objective, "search_queries": []string{query}})
		}},
		{"current-model", func() sdk.ToolResult { return nativeSearch(ctx, query, urls, false) }},
		{"exa-mcp", func() sdk.ToolResult {
			return mcpSearch(ctx, "mcp__exa__web_search_exa", map[string]any{"query": objective})
		}},
	})
}

func mcpSearch(ctx sdk.Context, name string, args map[string]any) sdk.ToolResult {
	// MCP servers connect in the background. Bound readiness waiting rather than
	// falling through before the primary server has had a chance to connect.
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		tools, err := ctx.Tools()
		if err != nil {
			return fail(fmt.Errorf("list callable tools: %w", err), map[string]any{"stopFallback": true})
		}
		ready := false
		for _, tool := range tools {
			if tool.Name == name {
				ready = true
				break
			}
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return fail(errors.New("MCP search cancelled"), map[string]any{"stopFallback": true})
		case <-deadline.C:
			return fail(fmt.Errorf("MCP search tool %s unavailable after 30 seconds", name), nil)
		case <-ticker.C:
		}
	}
	outcome, err := ctx.ExecuteTool(name, args, nil)
	if err != nil {
		// Host/transport failures are not provider errors; stop rather than bypass hooks.
		return fail(fmt.Errorf("execute %s: %w", name, err), map[string]any{"stopFallback": true})
	}
	return mcpOutcome(name, outcome)
}

func mcpOutcome(name string, outcome sdk.AgentToolCallOutcome) sdk.ToolResult {
	text := outcome.Result.Text()
	details := map[string]any{"mcpDetails": outcome.Result.Details, "structuredContent": outcome.Result.StructuredContent}
	failed := outcome.IsError || outcome.Result.IsError
	if outcome.Result.Terminate {
		details["stopFallback"] = true
	}
	if failed {
		// Unclassified hook errors may be permission refusals with arbitrary text.
		// Only known provider/connection failures are eligible for fallback.
		knownFailure := outcome.Result.StructuredContent != nil || text == "Tool "+name+" not found"
		for _, marker := range []string{"MCP", "HTTP", "connection refused", "dial tcp", "context deadline exceeded", "request timed out"} {
			knownFailure = knownFailure || strings.Contains(text, marker)
		}
		if !knownFailure {
			details["stopFallback"] = true
		}
		lower := strings.ToLower(text)
		for _, refusal := range []string{"blocked", "denied", "permission", "not allowed", "aborted", "cancelled", "canceled"} {
			if strings.Contains(lower, refusal) {
				details["stopFallback"] = true
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		structured := outcome.Result.StructuredContent
		if envelope, ok := structured.(map[string]any); ok {
			if _, hasContent := envelope["content"]; hasContent {
				structured = envelope["structuredContent"]
			}
		}
		if structured != nil {
			raw, err := json.Marshal(structured)
			if err != nil {
				return fail(fmt.Errorf("encode MCP search result: %w", err), nil)
			}
			text = string(raw)
		} else {
			text = "MCP search returned no content"
			failed = true
		}
	}
	return sdk.ToolResult{Content: text, Details: details, IsError: failed}
}
