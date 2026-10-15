package cursorcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as an account-free fake CLI executable. It checks the actual
// child environment, arguments, isolated workspace and permission configuration.
func TestMain(m *testing.M) {
	if scenario := os.Getenv("CURSOR_AUTH_TOKEN"); strings.HasPrefix(scenario, "fake-") {
		cwd, err := os.Getwd()
		if err != nil || cwd != os.Getenv("HOME") || os.Getenv("PIG_CURSOR_PARENT_SECRET") != "" || os.Getenv("CURSOR_API_ENDPOINT") != "https://api2.cursor.sh" {
			os.Exit(21)
		}
		data, err := os.ReadFile(filepath.Join(cwd, ".cursor", "cli-config.json"))
		if err != nil || !strings.Contains(string(data), `"Shell(*)"`) || !strings.Contains(string(data), `"Read(**)"`) {
			os.Exit(22)
		}
		args := strings.Join(os.Args[1:], " ")
		if strings.Contains(args, scenario) || !strings.Contains(args, "--mode ask --print --output-format stream-json --model test-model --workspace ") {
			os.Exit(23)
		}
		prompt, err := io.ReadAll(os.Stdin)
		if err != nil || string(prompt) != "test prompt" {
			os.Exit(24)
		}
		switch scenario {
		case "fake-ok":
			fmt.Println(`{"type":"system","subtype":"init"}`)
			fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"answer"}]}}`)
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"answer"}`)
		case "fake-error":
			fmt.Fprintln(os.Stderr, scenario+" private prompt should never appear")
			os.Exit(7)
		case "fake-partial":
			fmt.Println(`{"type":"assistant"}`)
		case "fake-tool":
			fmt.Println(`{"type":"tool_call","subtype":"started"}`)
		case "fake-wait":
			time.Sleep(time.Minute)
		default:
			os.Exit(25)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func config(t *testing.T) Config {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Config{CLI: executable, Models: []Model{{ID: "test-model", Name: "Test", ContextWindow: 100, MaxTokens: 20}}}
}

func TestRunIsolatedCLI(t *testing.T) {
	t.Setenv("PIG_CURSOR_PARENT_SECRET", "not-for-child")
	text, err := Run(t.Context(), config(t), "test-model", "fake-ok", "test prompt")
	if err != nil || text != "answer" {
		t.Fatalf("result %q, error %v", text, err)
	}
}

func TestRunFailuresNeverBecomeSuccess(t *testing.T) {
	for _, scenario := range []string{"fake-error", "fake-partial", "fake-tool"} {
		t.Run(scenario, func(t *testing.T) {
			text, err := Run(t.Context(), config(t), "test-model", scenario, "test prompt")
			if err == nil || text != "" {
				t.Fatalf("result %q, error %v", text, err)
			}
			if strings.Contains(err.Error(), scenario) || strings.Contains(err.Error(), "private prompt") {
				t.Fatal("credential or payload leaked")
			}
		})
	}
}

func TestRunCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, config(t), "test-model", "fake-wait", "test prompt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}

func TestRunRejectsMissingAuthUnknownModelAndOversizedPrompt(t *testing.T) {
	for _, test := range []struct{ model, token, prompt string }{
		{"test-model", "", "test prompt"}, {"other", "fake-ok", "test prompt"},
		{"test-model", "fake-ok", strings.Repeat("x", maxBytes+1)},
		{"test-model", "bad\ncredential", "test prompt"},
	} {
		if _, err := Run(t.Context(), config(t), test.model, test.token, test.prompt); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}

func TestDecodeProtocol(t *testing.T) {
	for _, input := range []string{
		"not json\n", `{"type":"result","subtype":"error","is_error":true}`, `{"type":"tool_call"}`,
		`{"type":"unknown"}`, `{"type":"assistant"}`, strings.Repeat("x", maxBytes+1),
		`{"type":"result","subtype":"success","is_error":false}`,
		`{"type":"result","subtype":"success","result":"answer"}`,
		"{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"answer\"}\n{\"type\":\"assistant\"}\n",
	} {
		if _, err := decode(strings.NewReader(input)); err == nil {
			t.Fatal("invalid stream accepted")
		}
	}
	reader := io.MultiReader(strings.NewReader("{\"type\":\"assistant\"}\n"), errorReader{})
	if _, err := decode(reader); err == nil {
		t.Fatal("read failure ignored")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("test read failure") }

func TestConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if cfg, err := Load(path); cfg != nil || err != nil {
		t.Fatalf("missing config: %v %v", cfg, err)
	}
	cfg := config(t)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := Load(path); err != nil || loaded.CLI != cfg.CLI {
		t.Fatalf("valid config: %v %v", loaded, err)
	}
	for _, input := range []string{`{}`, `{"unknown":true}`, string(data) + ` {}`, strings.Repeat("x", 65537)} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	cfg.Models = append(cfg.Models, cfg.Models[0])
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("duplicate model accepted")
	}
}

func TestTextPrompt(t *testing.T) {
	request := map[string]any{"systemPrompt": "be helpful", "messages": []any{
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "world"}}},
	}, "tools": []any{}}
	prompt, err := Prompt(request)
	if err != nil || !strings.Contains(prompt, "System instructions:\nbe helpful") || !strings.Contains(prompt, "assistant:\nworld") {
		t.Fatalf("prompt %q, %v", prompt, err)
	}
	request["tools"] = []any{map[string]any{"name": "bash"}}
	if _, err := Prompt(request); err == nil {
		t.Fatal("tools accepted")
	}
	delete(request, "tools")
	for _, message := range []any{
		map[string]any{"role": "toolResult", "content": "result"},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "image"}}},
		map[string]any{"role": "user", "content": strings.Repeat("x", maxBytes+1)},
	} {
		request["messages"] = []any{message}
		if _, err := Prompt(request); err == nil {
			t.Fatal("unsupported content accepted")
		}
	}
}

func TestTranscriptSystemAndToolChanges(t *testing.T) {
	request := map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "base", "sections": map[string]any{"rules": "current", "old": nil}},
		map[string]any{"role": "user", "content": "hello"},
	}}
	prompt, err := Prompt(request)
	if err != nil || !strings.Contains(prompt, "system:\nbase") || !strings.Contains(prompt, `"old":null`) || !strings.Contains(prompt, `"rules":"current"`) {
		t.Fatalf("system prompt %q, %v", prompt, err)
	}
	for _, key := range []string{"toolsAdded", "toolsRemoved"} {
		request["messages"] = []any{map[string]any{"role": "system", "content": "base", key: []any{map[string]any{"name": "bash"}}}}
		if _, err := Prompt(request); err == nil {
			t.Fatal("transcript tool changes accepted")
		}
	}
}
