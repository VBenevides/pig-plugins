package pigtest

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func post(t *testing.T, m *MockLLM, model string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"model": model})
	resp, err := http.Post(m.URL+"/chat/completions", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, sb.String()
}

func TestMockLLMServesScriptInOrderThenDone(t *testing.T) {
	m := NewMockLLM(Text("first"), HTTPError(429, "slow down"))
	defer m.Close()

	status, body := post(t, m, "any")
	if status != 200 || !strings.Contains(body, `"content":"first"`) || !strings.Contains(body, "[DONE]") {
		t.Fatalf("first reply: %d %s", status, body)
	}
	status, body = post(t, m, "any")
	if status != 429 || !strings.Contains(body, "slow down") {
		t.Fatalf("second reply: %d %s", status, body)
	}
	status, body = post(t, m, "any")
	if status != 200 || !strings.Contains(body, `"content":"done"`) {
		t.Fatalf("exhausted script must answer done: %d %s", status, body)
	}
	if got := len(m.Requests()); got != 3 {
		t.Fatalf("recorded %d requests, want 3", got)
	}
}

func TestMockLLMKeepsOneScriptPerModel(t *testing.T) {
	m := NewMockLLMByModel(map[string][]Reply{
		"a":      {Text("from a")},
		AnyModel: {Text("from any")},
	})
	defer m.Close()

	if _, body := post(t, m, "b"); !strings.Contains(body, "from any") {
		t.Fatalf("model without a script must use AnyModel: %s", body)
	}
	if _, body := post(t, m, "a"); !strings.Contains(body, "from a") {
		t.Fatalf("model a must use its own script: %s", body)
	}
}

func TestMockLLMEncodesToolCalls(t *testing.T) {
	m := NewMockLLM(Calls(Call("echo", map[string]any{"text": "hi"})))
	defer m.Close()

	_, body := post(t, m, "m")
	if !strings.Contains(body, `"name":"echo"`) || !strings.Contains(body, `\"text\":\"hi\"`) ||
		!strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("tool call not encoded: %s", body)
	}
}

// A Go extension must build, register and run through the real pig, and its tool result must reach the model.
func TestGoExtensionRunsThroughPig(t *testing.T) {
	RequirePig(t)
	ext, err := filepath.Abs(filepath.Join("..", "..", "testfixtures", "echo"))
	if err != nil {
		t.Fatal(err)
	}
	mock := NewMockLLM(Calls(Call("echo", map[string]any{"text": "ping"})), Text("done"))
	defer mock.Close()

	result := NewHome(t).Run(t, mock, RunOptions{Extensions: []string{ext}})
	if result.ExitCode != 0 {
		t.Fatalf("pig exited %d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if names := ToolNames(mock); !contains(names, "echo") {
		t.Fatalf("echo tool not offered to the model: %v", names)
	}
	if got := ToolResults(mock); len(got) != 1 || got[0] != "echo: ping" {
		t.Fatalf("tool results = %q, want [echo: ping]", got)
	}
}

func TestGoldenComparesExactly(t *testing.T) {
	Golden(t, "sample.golden", "line one\nline two\n")

	for name, got := range map[string]string{"changed": "line one\nline 2\n", "no trailing newline": "line one\nline two"} {
		probe := &recordingTB{TB: t}
		Golden(probe, "sample.golden", got)
		if !probe.failed {
			t.Fatalf("%s: Golden must fail on any difference", name)
		}
	}
	probe := &recordingTB{TB: t}
	Golden(probe, "does-not-exist.golden", "x")
	if !probe.failed {
		t.Fatal("a missing golden must fail the test")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// recordingTB captures Fatalf so the test can assert that Golden fails.
type recordingTB struct {
	testing.TB
	failed bool
}

func (r *recordingTB) Helper()               {}
func (r *recordingTB) Fatalf(string, ...any) { r.failed = true }
