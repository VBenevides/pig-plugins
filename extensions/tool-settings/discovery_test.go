package toolsettingsext

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func declarationNames(request map[string]any) []string {
	var names []string
	for _, raw := range request["tools"].([]any) {
		names = append(names, raw.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	return names
}

func TestFiveToolCoreAndDeferredLifecycle(t *testing.T) {
	pigtest.RequirePig(t)
	for _, target := range []string{"todo", "ask_user_question", "context_recover", "url_context", "web_search", "goal_finish", "browser_navigate", "ast_search", "lsp_symbols", "code_search"} {
		t.Run(target, func(t *testing.T) {
			home := pigtest.NewHome(t)
			if target == "browser_navigate" {
				path := filepath.Join(home.AgentDir(), toolsettings.FileName)
				cfg, err := toolsettings.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				cfg.EnabledTools[target] = true
				if err := toolsettings.Save(path, cfg); err != nil {
					t.Fatal(err)
				}
			}
			var extensions []string
			for _, name := range []string{"default-prompt", "tool-search", "todo", "ask-user-question", "context-prune", "web-search", "goal", "browser-use", "ast-search", "lsp", "tool-settings"} {
				path, err := filepath.Abs("../" + name)
				if err != nil {
					t.Fatal(err)
				}
				extensions = append(extensions, path)
			}
			replies := []pigtest.Reply{pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": target, "limit": 1})), pigtest.Text("loaded"), pigtest.Text("new request")}
			if target == "goal_finish" {
				replies = []pigtest.Reply{replies[0], pigtest.Calls(pigtest.Call("goal_finish", map[string]any{"outcome": "reached", "summary": "done"})), replies[1], replies[2]}
			}
			mock := pigtest.NewMockLLM(replies...)
			defer mock.Close()
			prompts := []string{"discover tool", "next request"}
			if target == "goal_finish" {
				prompts = append([]string{"/goal set inspect the repository"}, prompts...)
			}
			if target == "browser_navigate" {
				prompts = append([]string{"/browser-use mode chrome"}, prompts...)
			}
			result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensions, Prompts: prompts})
			for _, event := range result.Events {
				if event["type"] == "extension_error" {
					t.Fatalf("extension error: %v", event)
				}
			}
			requests := mock.Requests()
			if len(requests) != len(replies) {
				t.Fatalf("requests=%d", len(requests))
			}
			core := []string{"bash", "edit", "read", "tool_search", "write"}
			if got := declarationNames(requests[0]); !slices.Equal(got, core) {
				t.Fatalf("initial declarations=%v", got)
			}
			if !slices.Contains(declarationNames(requests[1]), target) {
				t.Fatalf("discovery did not load %s: %v", target, declarationNames(requests[1]))
			}
			if got := declarationNames(requests[len(requests)-1]); !slices.Equal(got, core) {
				t.Fatalf("next-request declarations=%v", got)
			}
		})
	}
}

func TestUnavailableDiscoveryKeepsFourTools(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	cfg, err := toolsettings.Load(filepath.Join(home.AgentDir(), toolsettings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DisabledTools["tool_search"] = true
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), cfg); err != nil {
		t.Fatal(err)
	}
	var extensions []string
	for _, name := range []string{"tool-search", "todo", "ast-search", "lsp", "web-search", "ask-user-question", "context-prune", "tool-settings"} {
		path, err := filepath.Abs("../" + name)
		if err != nil {
			t.Fatal(err)
		}
		extensions = append(extensions, path)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("done"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: extensions, Prompt: "hello"})
	requests := mock.Requests()
	if len(requests) != 1 || !slices.Equal(declarationNames(requests[0]), []string{"bash", "edit", "read", "write"}) {
		t.Fatalf("unexpected declarations: %v", requests)
	}
}
