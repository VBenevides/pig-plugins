package websearchext

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestPiGMCPFallbackSearch(t *testing.T) {
	pigtest.RequirePig(t)
	for _, parallelFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "parallel-primary", true: "exa-after-unsupported-current-model"}[parallelFails], func(t *testing.T) {
			home := pigtest.NewHome(t)
			mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "web_search", "limit": 3})), pigtest.Calls(pigtest.Call("web_search", map[string]any{"query": "query", "urls": []any{"https://example.com/docs"}})), pigtest.Text("done"))
			defer mock.Close()
			var parallelCalls, exaCalls atomic.Int32
			parallel := searchMCPFixture(t, "web_search", parallelFails, &parallelCalls)
			defer parallel.Close()
			exa := searchMCPFixture(t, "web_search_exa", false, &exaCalls)
			defer exa.Close()
			home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
			config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
				"parallel": map[string]any{"url": parallel.URL, "exposure": "deferred"},
				"exa":      map[string]any{"url": exa.URL, "exposure": "deferred"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(home.AgentDir(), "mcp.json"), config, 0600); err != nil {
				t.Fatal(err)
			}
			// Obsolete model overrides must not affect the current-model fallback.
			if err = os.WriteFile(filepath.Join(home.AgentDir(), "web-search.json"), []byte(`{"provider":"missing","model":"missing"}`), 0600); err != nil {
				t.Fatal(err)
			}
			ext, err := filepath.Abs(".")
			if err != nil {
				t.Fatal(err)
			}
			result := home.RunPig(t, pigtest.RunOptions{Extensions: []string{"builtin:mcp", "builtin:tool-search", ext}}, "-p", "--provider", "mock", "--model", "mock-model", "--tools", "read,bash,tool_search,web_search,mcp__parallel__web_search,mcp__exa__web_search_exa")
			if result.ExitCode != 0 {
				t.Fatalf("pig: %s %s", result.Stdout, result.Stderr)
			}
			results := pigtest.ToolResults(mock)
			if len(results) != 2 || !strings.Contains(results[1], "https://example.com/docs") {
				t.Fatalf("search result: %v; stderr=%s", results, result.Stderr)
			}
			if parallelCalls.Load() != 1 {
				t.Fatalf("Parallel calls: %d", parallelCalls.Load())
			}
			wantExa := int32(0)
			if parallelFails {
				wantExa = 1
			}
			if exaCalls.Load() != wantExa {
				t.Fatalf("Exa calls: %d, want %d", exaCalls.Load(), wantExa)
			}
		})
	}
}

func searchMCPFixture(t *testing.T, tool string, failed bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fixture", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": tool, "description": "Search", "inputSchema": map[string]any{"type": "object", "additionalProperties": true}}}}
		case "tools/call":
			calls.Add(1)
			if request.Params["name"] != tool {
				t.Errorf("tool: %v", request.Params)
			}
			args, _ := request.Params["arguments"].(map[string]any)
			key := "query"
			if tool == "web_search" {
				key = "objective"
			}
			if !strings.Contains(args[key].(string), "https://example.com/docs") {
				t.Errorf("URLs missing: %v", args)
			}
			text := "Docs: https://example.com/docs"
			if failed {
				text = "Search service unavailable"
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": failed}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
}
