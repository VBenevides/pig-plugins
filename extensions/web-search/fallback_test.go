package websearchext

import (
	"reflect"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestSearchFallbackOrder(t *testing.T) {
	for _, successAt := range []int{0, 1, 2, -1} {
		names := []string{"parallel-mcp", "current-model", "exa-mcp"}
		var called []string
		var attempts []searchAttempt
		for i, name := range names {
			attempts = append(attempts, searchAttempt{name, func() sdk.ToolResult {
				called = append(called, name)
				return sdk.ToolResult{Content: name, IsError: i != successAt}
			}})
		}
		result := runSearchChain(func() bool { return false }, attempts)
		count := successAt + 1
		if successAt < 0 {
			count = 3
		}
		if !reflect.DeepEqual(called, names[:count]) {
			t.Fatalf("order: %v", called)
		}
		if result.IsError != (successAt < 0) {
			t.Fatalf("result: %+v", result)
		}
		details := result.Details.(map[string]any)
		failures := details["attempts"].([]map[string]any)
		wantFailures := count - 1
		if successAt < 0 {
			wantFailures = count
		}
		if len(failures) != wantFailures {
			t.Fatalf("failures: %v", failures)
		}
		if successAt >= 0 && details["searchProvider"] != names[successAt] {
			t.Fatalf("provider: %v", details)
		}
		if successAt < 0 {
			for _, name := range names {
				if !strings.Contains(result.Content, name) {
					t.Fatalf("missing failure: %s", result.Content)
				}
			}
		}
	}
}

func TestSearchFallbackStops(t *testing.T) {
	for _, cancellation := range []bool{false, true} {
		cancelled := false
		called := 0
		result := runSearchChain(func() bool { return cancelled }, []searchAttempt{
			{"parallel", func() sdk.ToolResult {
				called++
				cancelled = cancellation
				return sdk.ToolResult{Content: "refused", IsError: true, Details: map[string]any{"stopFallback": !cancellation}}
			}},
			{"native", func() sdk.ToolResult { called++; return sdk.ToolResult{Content: "unexpected"} }},
		})
		if called != 1 || !result.IsError {
			t.Fatalf("continued after stop: %+v, calls=%d", result, called)
		}
	}
	result := runSearchChain(func() bool { return true }, []searchAttempt{{"parallel", func() sdk.ToolResult { t.Fatal("called after cancellation"); return sdk.ToolResult{} }}})
	if !result.IsError {
		t.Fatal("cancellation not reported")
	}
}

func TestMCPOutcomeFailures(t *testing.T) {
	for _, tc := range []struct {
		text                string
		structured          any
		terminate, wantStop bool
	}{
		{"Search unavailable", map[string]any{"isError": true}, false, false},
		{"HTTP 503", nil, false, false},
		{"Tool search not found", nil, false, false},
		{"user denied MCP search", nil, false, true},
		{"custom policy refusal", nil, false, true},
		{"Operation aborted", nil, false, true},
		{"HTTP 503", nil, true, true},
	} {
		t.Run(tc.text, func(t *testing.T) {
			result := mcpOutcome("search", sdk.AgentToolCallOutcome{IsError: true, Result: sdk.AgentToolResult{Content: []sdk.ToolResultContent{{Type: "text", Text: tc.text}}, StructuredContent: tc.structured, Terminate: tc.terminate}})
			if !result.IsError || (result.Details.(map[string]any)["stopFallback"] == true) != tc.wantStop {
				t.Fatalf("outcome: %+v", result)
			}
		})
	}
	empty := mcpOutcome("search", sdk.AgentToolCallOutcome{})
	if !empty.IsError {
		t.Fatal("empty result considered successful")
	}
	emptyEnvelope := mcpOutcome("search", sdk.AgentToolCallOutcome{Result: sdk.AgentToolResult{StructuredContent: map[string]any{"content": []any{}}}})
	if !emptyEnvelope.IsError {
		t.Fatal("empty MCP envelope considered successful")
	}
	structured := mcpOutcome("search", sdk.AgentToolCallOutcome{Result: sdk.AgentToolResult{StructuredContent: map[string]any{"content": []any{}, "structuredContent": map[string]any{"url": "https://example.com"}}}})
	if structured.IsError || structured.Content != `{"url":"https://example.com"}` {
		t.Fatalf("structured result: %+v", structured)
	}
}
