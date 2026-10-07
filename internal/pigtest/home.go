package pigtest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RequirePig skips the test when the `pig` binary is not on PATH.
func RequirePig(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("pig"); err != nil {
		t.Skip("pig is not on PATH")
	}
}

// Home is a temporary HOME plus a work directory for one pig session setup.
type Home struct {
	Dir  string // the isolated HOME
	Work string // working directory pig runs in
}

// NewHome creates an isolated HOME that the test framework removes.
func NewHome(t testing.TB) *Home {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &Home{Dir: dir, Work: filepath.Join(dir, "work")}
	if err := os.MkdirAll(h.Work, 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

// AgentDir is the agent directory pig uses inside this HOME.
func (h *Home) AgentDir() string { return filepath.Join(h.Dir, ".pig", "agent") }

// WriteModels registers each mock as an OpenAI-compatible provider in models.json: provider name to model ids.
func (h *Home) WriteModels(t testing.TB, providers map[string]ProviderModels) {
	t.Helper()
	entries := map[string]any{}
	for name, p := range providers {
		models := make([]any, len(p.Models))
		for i, id := range p.Models {
			models[i] = map[string]any{"id": id, "name": "Mock", "contextWindow": 100000, "maxTokens": 4096}
		}
		entries[name] = map[string]any{"baseUrl": p.Mock.URL, "apiKey": "x", "api": "openai-completions", "models": models}
	}
	data, err := json.Marshal(map[string]any{"providers": entries})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.AgentDir(), "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ProviderModels is one mock server and the model ids it serves.
type ProviderModels struct {
	Mock   *MockLLM
	Models []string
}

// RunOptions tune one pig invocation.
type RunOptions struct {
	// Extensions are paths passed as `-e`.
	Extensions []string
	// Prompt defaults to "go".
	Prompt string
	// Env adds or overrides environment variables.
	Env map[string]string
	// Timeout defaults to 90 seconds.
	Timeout time.Duration
}

// Result is the outcome of a pig run.
type Result struct {
	Stdout, Stderr string
	ExitCode       int
}

// Env builds the child environment: this HOME, none of the variables that would redirect pig's config root, and
// the real Go caches so an extension build is not cold in a fresh HOME.
func (h *Home) Env(extra map[string]string) []string {
	skip := map[string]bool{"HOME": true, "PIG_HOME": true, "PIG_CODING_AGENT_DIR": true,
		"PI_CODING_AGENT_DIR": true, "XDG_CONFIG_HOME": true}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !skip[key] {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+h.Dir)
	for key, value := range goCacheEnv() {
		env = append(env, key+"="+value)
	}
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func goCacheEnv() map[string]string {
	out := map[string]string{}
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		if os.Getenv(key) != "" {
			continue
		}
		if value, err := exec.Command("go", "env", key).Output(); err == nil {
			out[key] = string(bytes.TrimSpace(value))
		}
	}
	return out
}

// Run starts one print-mode session (`pig -p`) on provider "mock", model "mock-model", with the given extensions
// loaded, and returns its output. A non-zero exit code is a result, not a failure of Run.
func (h *Home) Run(t testing.TB, mock *MockLLM, opts RunOptions) Result {
	t.Helper()
	h.WriteModels(t, map[string]ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	return h.RunPig(t, opts, "-p", "--provider", "mock", "--model", "mock-model")
}

// RunPig runs `pig <args> [-e ext]... <prompt>` in the work directory.
func (h *Home) RunPig(t testing.TB, opts RunOptions, args ...string) Result {
	t.Helper()
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	prompt := opts.Prompt
	if prompt == "" {
		prompt = "go"
	}
	for _, ext := range opts.Extensions {
		args = append(args, "-e", ext)
	}
	args = append(args, prompt)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pig", args...)
	cmd.Dir = h.Work
	cmd.Env = h.Env(opts.Env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok || ctx.Err() != nil {
			t.Fatalf("pig %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, result.Stdout, result.Stderr)
		}
		result.ExitCode = exitErr.ExitCode()
	}
	return result
}
