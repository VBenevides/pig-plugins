package browseruseext

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func probePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("testdata/session-probe")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewSessionReloadAndProcessRestartResetOff(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: append(extensionPaths(t), probePath(t)), Prompts: []string{
		"/browser-use mode chrome", "enabled", "/browser-test-new", "new off", "/browser-use mode firefox", "enabled firefox", "/browser-test-reload", "reloaded off",
	}})
	for _, event := range result.Events {
		if event["type"] == "extension_error" {
			t.Fatalf("lifecycle error: %v", event)
		}
	}
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests %d; notices %q stderr %s", len(requests), result.Notices(), result.Stderr)
	}
	for i, req := range requests {
		raw, _ := json.Marshal(req["tools"])
		if strings.Contains(string(raw), "browser_snapshot") != (i == 0 || i == 2) {
			t.Errorf("request %d: tools %s", i, raw)
		}
	}
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	resumed := home.RunPig(t, pigtest.RunOptions{Extensions: extensionPaths(t), Prompt: "resumed off"}, "-p", "--continue", "--provider", "mock", "--model", "mock-model")
	if resumed.ExitCode != 0 {
		t.Fatalf("resume failed: %s", resumed.Stderr)
	}
	requests = mock.Requests()
	raw, _ := json.Marshal(requests[len(requests)-1]["tools"])
	if strings.Contains(string(raw), "browser_snapshot") {
		t.Fatal("resume restored browser mode")
	}
}

type webdriverFixture struct {
	mu    sync.Mutex
	calls []string
}

func (d *webdriverFixture) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, r.Method+" "+r.URL.Path)
	var input map[string]any
	if r.Method == "POST" {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "fixture JSON", 400)
			return
		}
	}
	var value any
	switch r.URL.Path {
	case "/session":
		value = map[string]any{"sessionId": "test-session"}
	case "/session/test-session/execute/sync":
		args, _ := input["args"].([]any)
		if len(args) == 0 {
			value = map[string]any{"title": "Test form", "text": "Untrusted page text", "elements": []any{map[string]any{"element": map[string]string{"element-6066-11e4-a52e-4f735466cecf": "input-1"}, "tag": "input", "type": "text", "label": "Name"}}}
		} else {
			value = true
		}
	case "/session/test-session/screenshot":
		value = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a2ioAAAAASUVORK5CYII="
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
}

func TestRealHostConfirmationScreenshotsAndCleanup(t *testing.T) {
	pigtest.RequirePig(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			driver := &webdriverFixture{}
			server := httptest.NewServer(http.HandlerFunc(driver.serve))
			defer server.Close()
			home := pigtest.NewHome(t)
			replies := []pigtest.Reply{pigtest.Calls(pigtest.Call("browser_navigate", map[string]any{"url": "https://example.org"}))}
			if allow {
				replies = append(replies,
					pigtest.Calls(pigtest.Call("browser_snapshot", map[string]any{})),
					pigtest.Calls(pigtest.Call("browser_fill", map[string]any{"ref": "r4943e43bc034-2-1", "value": "Test value"})),
					pigtest.Calls(pigtest.Call("browser_screenshot", map[string]any{})),
				)
			}
			replies = append(replies, pigtest.Text("done"))
			mock := pigtest.NewMockLLM(replies...)
			defer mock.Close()
			result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: extensionPaths(t), Vision: true, Env: map[string]string{"PIG_BROWSER_CHROME_DRIVER_URL": server.URL}, Confirm: func(map[string]any) bool { return allow }, Prompts: []string{"/browser-use mode chrome", "use the test browser", "/browser-use mode off"}})
			expectedDialogs := 1
			if allow {
				expectedDialogs = 2
			}
			if len(result.Asked) != expectedDialogs {
				t.Fatalf("dialogs %d, expected %d; notices %q stderr %s", len(result.Asked), expectedDialogs, result.Notices(), result.Stderr)
			}
			driver.mu.Lock()
			calls := append([]string(nil), driver.calls...)
			driver.mu.Unlock()
			if !allow {
				if len(calls) != 0 {
					t.Fatalf("declined action reached driver: %v", calls)
				}
				return
			}
			if calls[len(calls)-1] != "DELETE /session/test-session" {
				t.Fatalf("missing cleanup: %v", calls)
			}
			raw, _ := json.Marshal(mock.Requests()[4]["messages"])
			if !strings.Contains(string(raw), "image_url") || !strings.Contains(string(raw), "data:image/png;base64,") {
				t.Fatalf("model did not receive screenshot: %s", raw)
			}
			for _, event := range result.Events {
				if event["type"] == "tool_execution_end" && event["isError"] == true {
					t.Fatalf("tool failed: %v", event)
				}
			}
		})
	}
}

func TestAskModeStillBlocksBrowserActions(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("browser_navigate", map[string]any{"url": "https://example.org"})), pigtest.Text("blocked"))
	defer mock.Close()
	ask, err := filepath.Abs("../ask-mode")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: append(extensionPaths(t), ask), Prompts: []string{"/browser-use mode chrome", "/mode ask", "attempt navigation"}})
	raw, _ := json.Marshal(mock.Requests()[1]["messages"])
	if !strings.Contains(string(raw), "Ask mode is on") {
		t.Fatalf("Ask mode bypassed: %s; stderr %s", raw, result.Stderr)
	}
}

func TestFusedBrowserModeVisibility(t *testing.T) {
	binary := os.Getenv("PIG_FUSED_TEST_BINARY")
	if binary == "" {
		t.Skip("set PIG_FUSED_TEST_BINARY to a candidate fused binary")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "pig")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := pigtest.NewHome(t)
	if err := os.MkdirAll(home.AgentDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"check-update.json": `{"enabled":false}`, "auto-model.json": `{"primary":{"provider":"mock","model":"mock-model","thinking":"off"},"fallback":{"provider":"mock","model":"mock-model","thinking":"off"}}`} {
		if err := os.WriteFile(filepath.Join(home.AgentDir(), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{probePath(t)}, Prompts: []string{"off", "/browser-use mode firefox", "enabled", "/browser-test-new", "new off"}})
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests %d notices %q stderr %s", len(requests), result.Notices(), result.Stderr)
	}
	for i, req := range requests {
		raw, _ := json.Marshal(req["tools"])
		if strings.Contains(string(raw), "browser_snapshot") != (i == 1) {
			t.Fatalf("fused visibility %d: %s", i, raw)
		}
	}
}
