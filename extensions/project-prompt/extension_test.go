package projectprompt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestMissingBaseFailsVisibly(t *testing.T) {
	for _, data := range []map[string]any{nil, {"systemPrompt": 12}} {
		if result, err := appendRules(sdk.Context{}, data); result != nil || err == nil {
			t.Fatalf("invalid base accepted: %v, %v", result, err)
		}
	}
}

// TestEffectivePrompt captures real host requests, not merely handler output.
func TestEffectivePrompt(t *testing.T) {
	pigtest.RequirePig(t)
	_, file, _, _ := runtime.Caller(0)
	extensions := filepath.Dir(filepath.Dir(file))
	loaded := []string{
		filepath.Join(extensions, "hashline-edit"),
		filepath.Join(extensions, "todo"),
		filepath.Join(extensions, "ask-user-question"),
		filepath.Join(extensions, "web-search"),
	}
	home := pigtest.NewHome(t)
	skillDir := filepath.Join(home.AgentDir(), "skills", "prompt-smoke")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: prompt-smoke\ndescription: HOST_SKILL_SENTINEL\n---\nSmoke skill.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.Work, "AGENTS.md"), []byte("HOST_PROJECT_CONTEXT_SENTINEL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: loaded, Prompts: []string{"baseline"}})
	baseline := mock.Requests()
	if len(baseline) != 1 {
		t.Fatalf("baseline request count: %d", len(baseline))
	}
	base := systemText(t, baseline[0])
	for _, required := range []string{"<tools>", "<rules>", "<docs>", "HOST_PROJECT_CONTEXT_SENTINEL", "<skills>", "HOST_SKILL_SENTINEL", home.Work} {
		if !strings.Contains(base, required) {
			t.Fatalf("baseline lacks %q", required)
		}
	}
	baseNames := toolNames(t, baseline[0])
	for _, name := range []string{"read", "edit", "write", "bash", "todo", "ask_user_question", "web_search"} {
		if !slices.Contains(baseNames, name) {
			t.Fatalf("baseline lacks tool %q: %v", name, baseNames)
		}
	}
	loaded = append(loaded, filepath.Join(extensions, "project-prompt"))
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: loaded, Prompts: []string{"first adapted run", "second adapted run"}})
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("effective request count: %d", len(requests))
	}
	for i, request := range requests[1:] {
		got := systemText(t, request)
		if got != base {
			t.Fatalf("run %d changed the prompt without project instructions", i+1)
		}
		if names := toolNames(t, request); !slices.Equal(names, baseNames) {
			t.Fatalf("tools changed: %v, baseline %v", names, baseNames)
		}
	}
	t.Logf("2 effective requests preserved all %d baseline bytes and retained tools %v", len(base), baseNames)
}

func systemText(t *testing.T, request map[string]any) string {
	t.Helper()
	messages, ok := request["messages"].([]any)
	if !ok {
		t.Fatal("request has no messages")
	}
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] == "system" {
			text, ok := message["content"].(string)
			if !ok {
				t.Fatal("system content is not text")
			}
			return text
		}
	}
	t.Fatal("request has no system message")
	return ""
}

func toolNames(t *testing.T, request map[string]any) []string {
	t.Helper()
	tools, ok := request["tools"].([]any)
	if !ok {
		t.Fatal("request has no tools")
	}
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		if name == "" {
			t.Fatal("tool has no function name")
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestLocalDefaultsAtProviderBoundary(t *testing.T) {
	pigtest.RequirePig(t)
	_, file, _, _ := runtime.Caller(0)
	extension := filepath.Dir(file)
	for _, tc := range []struct {
		name       string
		trusted    bool
		present    bool
		hostLoaded bool
	}{
		{name: "literal files", trusted: true, present: true},
		{name: "absent literal dot-local", trusted: true},
		{name: "untrusted", present: true},
		{name: "already loaded by host", trusted: true, present: true, hostLoaded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := pigtest.NewHome(t)
			write := func(path, content string) {
				t.Helper()
				full := filepath.Join(home.Work, path)
				if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("local/APPEND_SYSTEM.md", "WRONG_SPELLING_MUST_NOT_LOAD")
			appendContent, agentsContent := "LITERAL_APPEND_SENTINEL\n", "LOCAL_AGENTS_SENTINEL\n"
			if tc.present {
				write(".local/APPEND_SYSTEM.md", appendContent)
				write("local/AGENTS.md", agentsContent)
			}
			if tc.hostLoaded {
				write("AGENTS.md", appendContent+agentsContent)
			} else {
				write("AGENTS.md", "HOST_CONTEXT_SENTINEL\n")
			}
			if err := os.MkdirAll(home.AgentDir(), 0o700); err != nil {
				t.Fatal(err)
			}
			trust, err := json.Marshal(map[string]bool{home.Work: tc.trusted})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home.AgentDir(), "trust.json"), trust, 0o600); err != nil {
				t.Fatal(err)
			}
			// Projects without local resources are automatically trusted by the host.
			write(".pig/settings.json", "{}\n")
			mock := pigtest.NewMockLLM(pigtest.Text("ok"))
			defer mock.Close()
			home.RunRPC(t, mock, pigtest.RPCOptions{Prompts: []string{"baseline"}})
			base := systemText(t, mock.Requests()[0])
			baseTools := toolNames(t, mock.Requests()[0])
			home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extension}})
			if len(mock.Requests()) != 1 {
				t.Fatal("loading project-prompt made an automatic model call")
			}
			home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extension}, Prompts: []string{"first", "second"}})
			requests := mock.Requests()
			if len(requests) != 3 {
				t.Fatalf("expected exactly one model request per prompt, got %d", len(requests)-1)
			}
			for _, request := range requests[1:] {
				got := systemText(t, request)
				if !strings.HasPrefix(got, base) {
					t.Fatal("host base changed")
				}
				if strings.Contains(got, "WRONG_SPELLING_MUST_NOT_LOAD") {
					t.Fatal("silently substituted local for .local")
				}
				for _, content := range []string{appendContent, agentsContent} {
					want := 0
					if tc.hostLoaded || (tc.trusted && tc.present) {
						want = 1
					}
					if count := strings.Count(got, content); count != want {
						t.Fatalf("instruction %q count %d, want %d", content, count, want)
					}
				}
				if tc.trusted && tc.present && !tc.hostLoaded {
					for _, path := range []string{".local/APPEND_SYSTEM.md", "local/AGENTS.md"} {
						if !strings.Contains(got, "# Trusted project instructions: "+path) {
							t.Fatalf("missing provenance for %s", path)
						}
					}
				}
				if !slices.Equal(toolNames(t, request), baseTools) {
					t.Fatal("local defaults changed the tool surface")
				}
			}
		})
	}
}

func TestChangedDefaultsReachNextRequest(t *testing.T) {
	pigtest.RequirePig(t)
	_, file, _, _ := runtime.Caller(0)
	home := pigtest.NewHome(t)
	for _, path := range []string{".local/APPEND_SYSTEM.md", "local/AGENTS.md"} {
		full := filepath.Join(home.Work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("INITIAL_"+path+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(home.AgentDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	trust, err := json.Marshal(map[string]bool{home.Work: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "trust.json"), trust, 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("ok"), pigtest.Text("ok"))
	defer mock.Close()
	target, err := url.Parse(strings.TrimSuffix(mock.URL, "/v1"))
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			for _, path := range []string{".local/APPEND_SYSTEM.md", "local/AGENTS.md"} {
				if err := os.WriteFile(filepath.Join(home.Work, path), []byte("CHANGED_"+path+"\n"), 0o600); err != nil {
					t.Errorf("change instructions: %v", err)
					http.Error(w, "cannot change instructions", http.StatusInternalServerError)
					return
				}
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	mock.URL = server.URL + "/v1"
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{filepath.Dir(file)}, Prompts: []string{"first", "second"}})
	requests := mock.Requests()
	if len(requests) != 2 {
		t.Fatalf("expected two requests, got %d", len(requests))
	}
	for i, request := range requests {
		got := systemText(t, request)
		prefix, absent := "INITIAL_", "CHANGED_"
		if i == 1 {
			prefix, absent = absent, prefix
		}
		for _, path := range []string{".local/APPEND_SYSTEM.md", "local/AGENTS.md"} {
			if strings.Count(got, prefix+path+"\n") != 1 || strings.Contains(got, absent+path+"\n") {
				t.Fatalf("request %d did not consume the current %s exactly once", i+1, path)
			}
		}
	}
}

func TestLocalInstructionReadFailures(t *testing.T) {
	for _, kind := range []string{"directory", "oversize", "outside symlink", "invalid UTF-8", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			cwd := t.TempDir()
			dir := filepath.Join(cwd, ".local")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "APPEND_SYSTEM.md")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "oversize":
				err = os.WriteFile(path, []byte(strings.Repeat("x", maxLocalInstructionBytes+1)), 0o600)
			case "outside symlink":
				outside := filepath.Join(t.TempDir(), "instructions")
				if err := os.WriteFile(outside, []byte("OUTSIDE"), 0o600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(outside, path)
			case "invalid UTF-8":
				err = os.WriteFile(path, []byte{0xff}, 0o600)
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root can read files with no permission bits")
				}
				err = os.WriteFile(path, []byte("UNREADABLE"), 0o000)
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := appendPrompt("base", cwd, true)
			if result != nil || err == nil || !strings.Contains(err.Error(), ".local/APPEND_SYSTEM.md") {
				t.Fatalf("failure not reported with provenance: %v, %v", result, err)
			}
			if _, err := appendPrompt("base", cwd, false); err != nil {
				t.Fatalf("untrusted instruction source accessed: %v", err)
			}
		})
	}
}
