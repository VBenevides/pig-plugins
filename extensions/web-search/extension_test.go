package websearchext

import (
	"encoding/json"
	"fmt"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPiGProviderNativeSearch(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "web_search", "limit": 1})), pigtest.Calls(pigtest.Call("web_search", map[string]any{"query": "query"})), pigtest.Text("done"))
	defer mock.Close()
	fixture, err := os.ReadFile("../../testfixtures/websearch.json")
	if err != nil {
		t.Fatal(err)
	}
	var events map[string][]map[string]any
	if err = json.Unmarshal(fixture, &events); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("not native Responses route: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
			t.Errorf("no native search tool: %v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events["openai"] {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
	}))
	defer server.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}, "search": {Mock: &pigtest.MockLLM{URL: server.URL + "/v1"}, Models: []string{"search-model"}}})
	path := filepath.Join(home.AgentDir(), "models.json")
	raw, _ := os.ReadFile(path)
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["providers"].(map[string]any)["search"].(map[string]any)["api"] = "openai-responses"
	raw, _ = json.Marshal(config)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home.AgentDir(), "web-search.json"), []byte(`{"provider":"search","model":"search-model"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunPig(t, pigtest.RunOptions{Extensions: []string{"builtin:tool-search", ext}}, "-p", "--provider", "mock", "--model", "mock-model", "--tools", "read,bash,tool_search,web_search")
	if result.ExitCode != 0 {
		t.Fatalf("pig: %s %s", result.Stdout, result.Stderr)
	}
	toolResults := pigtest.ToolResults(mock)
	if len(toolResults) != 2 || !strings.Contains(toolResults[0], "web_search") || !strings.Contains(toolResults[1], "Hi 😀[1] world") || !strings.Contains(toolResults[1], "https://example.com/docs") {
		t.Fatalf("uncited search result: %v; stderr=%s", toolResults, result.Stderr)
	}
	if slices.Contains(pigtest.ToolNames(mock), "web_search") {
		t.Fatal("search active before discovery")
	}
	if slices.Contains(pigtest.ToolNames(mock), "url_context") {
		t.Fatal("url_context active for unsupported conversation model")
	}
}
