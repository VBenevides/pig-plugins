package picurator_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

const marker = "zebra-fsync-4711"

func extensionPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func curatorBin(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("PI_CURATOR_BIN"); bin != "" {
		return bin
	}
	bin, err := exec.LookPath("curator")
	if err != nil {
		t.Skip("the curator program is not installed (set PI_CURATOR_BIN or put curator on PATH)")
	}
	return bin
}

// repoHome returns a test home whose working directory is a git repository.
func repoHome(t *testing.T) *pigtest.Home {
	t.Helper()
	pigtest.RequirePig(t)
	curatorBin(t)
	home := pigtest.NewHome(t)
	if out, err := exec.Command("git", "init", "-q", home.Work).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return home
}

func runCurator(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(curatorBin(t), args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("curator %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// seed initializes the repository's memory and stores two events in an earlier session.
func seed(t *testing.T, home *pigtest.Home) {
	t.Helper()
	runCurator(t, "", "init", "--consent", "--cwd", home.Work)
	events := []map[string]any{
		{"id": "decision", "session_id": "history", "category": "user_message",
			"content": "Decision: " + marker + " keeps atomic writes durable by syncing before the rename"},
		{"id": "noise", "session_id": "history2", "category": "user_message",
			"content": "Decision: sorting uses case insensitive ordering"},
	}
	payload, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		t.Fatal(err)
	}
	runCurator(t, string(payload), "ingest", "--cwd", home.Work)
}

func journal(home *pigtest.Home) string {
	data, _ := os.ReadFile(filepath.Join(home.Work, ".curator", "journal", "events.jsonl"))
	return string(data)
}

// env pins every pi-curator variable so the developer's own environment cannot change a run.
func env(overrides map[string]string) map[string]string {
	out := map[string]string{
		"PI_CURATOR_PREFETCH_BUDGET":   "0",
		"PI_CURATOR_STARTUP_DECISIONS": "0",
		"PI_CURATOR_SEARCH_ENGINE":     "legacy",
		"PI_CURATOR_PREFETCH_ENGINE":   "legacy",
	}
	for key, value := range overrides {
		out[key] = value
	}
	return out
}

func requestText(mock *pigtest.MockLLM) string {
	var all strings.Builder
	for _, request := range mock.Requests() {
		encoded, _ := json.Marshal(request)
		all.Write(encoded)
	}
	return all.String()
}

func TestSearchThenReadEndToEnd(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("memory_search", map[string]any{"query": "atomic writes durable"})),
		pigtest.Calls(pigtest.Call("memory_read", map[string]any{"ids": []string{"decision"}, "budget": 256})),
		pigtest.Text("done"),
	)
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "How did we make writes durable?", Env: env(nil)})
	if result.ExitCode != 0 {
		t.Fatalf("exit %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	names := strings.Join(pigtest.ToolNames(mock), " ")
	if !strings.Contains(names, "memory_search") || !strings.Contains(names, "memory_read") {
		t.Errorf("memory tools are not offered to the model: %s", names)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 2 {
		t.Fatalf("tool results = %q\nstderr:\n%s", results, result.Stderr)
	}
	if !strings.Contains(results[0], "ID decision |") || !strings.Contains(results[0], marker) || strings.Contains(results[0], "case insensitive") {
		t.Errorf("search result = %q", results[0])
	}
	var page struct {
		EventID    string `json:"event_id"`
		Content    string `json:"content"`
		Complete   bool   `json:"complete"`
		NextCursor int    `json:"next_cursor"`
		Untrusted  bool   `json:"untrusted"`
	}
	if err := json.Unmarshal([]byte(results[1]), &page); err != nil {
		t.Fatalf("read result is not the curator page: %v\n%s", err, results[1])
	}
	if page.EventID != "decision" || !strings.Contains(page.Content, marker) || !page.Complete || !page.Untrusted {
		t.Errorf("page = %+v", page)
	}
}

func TestReadPagesWithCursor(t *testing.T) {
	home := repoHome(t)
	runCurator(t, "", "init", "--consent", "--cwd", home.Work)
	text := strings.Repeat("snow \u96ea\U0001F600<&\"\\\n", 400)
	payload, _ := json.Marshal(map[string]any{"events": []map[string]any{{"id": "long", "session_id": "s", "category": "user_message", "content": text}}})
	runCurator(t, string(payload), "ingest", "--cwd", home.Work)

	calls := []pigtest.ToolCall{pigtest.Call("memory_read", map[string]any{"ids": []string{"long"}, "budget": 256})}
	mock := pigtest.NewMockLLM(pigtest.Calls(calls...), pigtest.Text("done"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "read it", Env: env(nil)})
	results := pigtest.ToolResults(mock)
	if len(results) != 1 {
		t.Fatalf("results = %q", results)
	}
	var first struct {
		Complete   bool `json:"complete"`
		NextCursor int  `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(results[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Complete || first.NextCursor <= 0 {
		t.Fatalf("a 256-token page of a long event must be incomplete with a next_cursor: %s", results[0])
	}
	if got := (len(results[0]) + 1 + 3) / 4; got > 256 {
		t.Errorf("page costs %d estimated tokens, budget 256", got)
	}

	next := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("memory_read", map[string]any{"ids": []string{"long"}, "cursor": first.NextCursor, "budget": 256})), pigtest.Text("done"))
	defer next.Close()
	home.Run(t, next, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "read more", Env: env(nil)})
	second := pigtest.ToolResults(next)
	if len(second) != 1 || !strings.Contains(second[0], `"cursor":`+jsonNumber(first.NextCursor)) {
		t.Errorf("the second page does not start at the cursor: %q", second)
	}
}

func jsonNumber(n int) string {
	encoded, _ := json.Marshal(n)
	return string(encoded)
}

func TestInvalidToolRequestsAreRejectedBeforeCurator(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	mock := pigtest.NewMockLLM(
		pigtest.Calls(
			pigtest.Call("memory_search", map[string]any{"query": "atomic", "budget": 1}),
			pigtest.Call("memory_search", map[string]any{"query": "  "}),
			pigtest.Call("memory_read", map[string]any{"ids": []string{"decision"}, "budget": 255}),
			pigtest.Call("memory_read", map[string]any{"ids": []string{"-x"}}),
			pigtest.Call("memory_read", map[string]any{"ids": []string{"a", "b"}, "cursor": 5}),
			pigtest.Call("memory_read", map[string]any{"ids": []string{"decision"}, "cursor": -1}),
		),
		pigtest.Text("done"),
	)
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "go", Env: env(nil)})
	want := []string{
		"budget must be an integer in 64..8192 estimated tokens",
		"query must be nonempty",
		"budget must be an integer in 256..8192 estimated tokens",
		"ids must contain 1..5 event IDs",
		"cursor must be nonnegative and applies to a single event",
		"cursor must be nonnegative and applies to a single event",
	}
	results := pigtest.ToolResults(mock)
	if len(results) != len(want) {
		t.Fatalf("results = %q", results)
	}
	for i, message := range want {
		if !strings.Contains(results[i], message) {
			t.Errorf("result %d = %q, want %q", i, results[i], message)
		}
	}
}

func TestPrefetchBudgetZeroInjectsNoHistory(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "How did we implement atomic writes durable?", Env: env(nil)})
	text := requestText(mock)
	if strings.Contains(text, marker) {
		t.Error("prefetch 0 and startup 0 must not put stored history into the request")
	}
	if !strings.Contains(text, "Repository memory") {
		t.Error("the static recall guidance is missing from the system prompt")
	}
}

func TestDefaultPrefetchInjectsHistoryOutsideSystemPrompt(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "How did we implement atomic writes durable?",
		Env: env(map[string]string{"PI_CURATOR_PREFETCH_BUDGET": "512"})})
	requests := mock.Requests()
	if len(requests) == 0 {
		t.Fatal("no request reached the model")
	}
	messages, _ := requests[0]["messages"].([]any)
	for _, raw := range messages {
		msg, _ := raw.(map[string]any)
		encoded, _ := json.Marshal(msg["content"])
		hasMarker := strings.Contains(string(encoded), marker)
		switch msg["role"] {
		case "system":
			if hasMarker {
				t.Error("stored history reached the system prompt")
			}
		case "user":
			if hasMarker {
				return
			}
		}
	}
	t.Errorf("the prefetched history is not in a user-role message of the request")
}

func TestStartupDecisionsAreOptInAndCapped(t *testing.T) {
	home := repoHome(t)
	seed(t, home)
	run := func(count string) string {
		mock := pigtest.NewMockLLM(pigtest.Text("ok"))
		defer mock.Close()
		home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "hi", Env: env(map[string]string{"PI_CURATOR_STARTUP_DECISIONS": count})})
		return requestText(mock)
	}
	if text := run("0"); strings.Contains(text, "case insensitive ordering") {
		t.Error("startup 0 must add nothing")
	}
	text := run("1")
	if !strings.Contains(text, "Recent decisions recorded in earlier sessions") {
		t.Fatalf("startup 1 added no decisions block")
	}
	if strings.Contains(text, marker) == strings.Contains(text, "case insensitive ordering") {
		t.Errorf("startup 1 must show exactly one decision")
	}
	if text := run("2"); !strings.Contains(text, marker) || !strings.Contains(text, "case insensitive ordering") {
		t.Error("startup 2 must show both decisions")
	}
}

func TestUninitializedRepositoryDeniesMemoryAndCreatesNothing(t *testing.T) {
	home := repoHome(t)
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("memory_search", map[string]any{"query": "anything"})),
		pigtest.Text("done"),
	)
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "go", Env: env(nil)})
	if _, err := os.Stat(filepath.Join(home.Work, ".curator")); err == nil {
		t.Error(".curator was created without consent")
	}
	// The tool is only offered once memory is ready, so the model cannot even call it.
	for _, name := range pigtest.ToolNames(mock) {
		if strings.HasPrefix(name, "memory_") {
			t.Errorf("tool %s offered without consented memory\nstderr:\n%s", name, result.Stderr)
		}
	}
}

func TestConsentIsAskedAndCaptureIsDurable(t *testing.T) {
	home := repoHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("the flag is --fast"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil),
		Prompts: []string{"remember the deploy flag"}, Confirm: func(map[string]any) bool { return true }})
	if len(result.Asked) != 1 || !strings.Contains(result.Asked[0]["title"].(string), "create repository memory?") {
		t.Fatalf("asked = %v\nstderr:\n%s", result.Asked, result.Stderr)
	}
	log := journal(home)
	for _, want := range []string{"remember the deploy flag", "the flag is --fast", `"task_boundary"`} {
		if !strings.Contains(log, want) {
			t.Errorf("journal lacks %q:\n%s", want, log)
		}
	}
	if !strings.Contains(strings.Join(result.Notices(), "\n"), "capturing this session") {
		t.Errorf("notices = %q", result.Notices())
	}
}

func TestDecliningCreatesNothing(t *testing.T) {
	home := repoHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(nil),
		Prompts: []string{"hello"}, Confirm: func(map[string]any) bool { return false }})
	if len(result.Asked) != 1 {
		t.Fatalf("asked = %v", result.Asked)
	}
	if _, err := os.Stat(filepath.Join(home.Work, ".curator")); err == nil {
		t.Error("declining must not create .curator")
	}
}

func TestMissingCuratorDoesNotStopTheSession(t *testing.T) {
	home := repoHome(t)
	mock := pigtest.NewMockLLM(pigtest.Text("still here"))
	defer mock.Close()
	missing := map[string]string{"PI_CURATOR_BIN": filepath.Join(home.Dir, "no-such-curator")}
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "go", Env: env(missing)})
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, "still here") {
		t.Fatalf("session did not complete: exit %d\n%s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if _, err := os.Stat(filepath.Join(home.Work, ".curator")); err == nil {
		t.Error(".curator was created without a working curator")
	}

	rpcMock := pigtest.NewMockLLM(pigtest.Text("ok"))
	defer rpcMock.Close()
	rpc := home.RunRPC(t, rpcMock, pigtest.RPCOptions{Extensions: []string{extensionPath(t)}, Env: env(missing), Prompts: []string{"go"}})
	if notices := strings.Join(rpc.Notices(), "\n"); !strings.Contains(notices, "setup failed, not capturing") {
		t.Errorf("the failure is not shown to the user: %q", rpc.Notices())
	}
}
