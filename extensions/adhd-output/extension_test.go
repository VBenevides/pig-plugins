package adhdoutput

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	mode "github.com/VBenevides/pig-plugins/internal/adhdoutput"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

func TestDefaultFlagValues(t *testing.T) {
	for _, test := range []struct {
		name      string
		value     any
		want      bool
		wantError bool
	}{
		{name: "missing", value: nil},
		{name: "bare flag", value: true, want: true},
		{name: "boolean false", value: false},
		{name: "explicit true", value: "true", want: true},
		{name: "explicit false", value: "false"},
		{name: "empty", value: "", wantError: true},
		{name: "invalid text", value: "yes", wantError: true},
		{name: "numeric string", value: "1", wantError: true},
		{name: "number", value: float64(1), wantError: true},
		{name: "uppercase", value: "TRUE", wantError: true},
		{name: "whitespace", value: " true ", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseDefaultFlag(test.value)
			if got != test.want || (err != nil) != test.wantError {
				t.Fatalf("parseDefaultFlag(%v): got %v, %v; want %v, error=%v", test.value, got, err, test.want, test.wantError)
			}
		})
	}
}

func extensionPath(t testing.TB) string {
	t.Helper()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func probePath(t testing.TB) string {
	return filepath.Join(extensionPath(t), "testdata", "session-probe")
}
func requestText(t testing.TB, request map[string]any) string {
	t.Helper()
	data, err := json.Marshal(request["messages"])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func systemText(t testing.TB, request map[string]any) string {
	t.Helper()
	messages, ok := request["messages"].([]any)
	if !ok {
		t.Fatal("provider request has no messages")
	}
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] == "system" {
			text, ok := message["content"].(string)
			if !ok {
				t.Fatal("system message has no text")
			}
			return text
		}
	}
	t.Fatal("provider request has no system message")
	return ""
}
func assertNoErrors(t testing.TB, result pigtest.RPCResult) {
	t.Helper()
	for _, event := range result.Events {
		if event["type"] == "extension_error" || event["notifyType"] == "error" || (event["type"] == "response" && event["success"] == false) {
			t.Fatalf("host/extension error: %v\nstderr: %s", event, result.Stderr)
		}
	}
}

// TestProviderBoundToggle exercises the native host and captures real HTTP requests.
// The default rules come from the production go:embed file, not a test copy.
func TestProviderBoundToggle(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mustWrite := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(home.AgentDir(), "SYSTEM.md"):        "",
		filepath.Join(home.AgentDir(), "APPEND_SYSTEM.md"): "UNCHANGED_APPEND_SYSTEM_SENTINEL\nNever bypass safety checks.",
		filepath.Join(home.Work, "AGENTS.md"):              "UNCHANGED_PROJECT_INSTRUCTIONS_SENTINEL",
	}
	for path, content := range files {
		mustWrite(path, content)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("baseline"), pigtest.Text("off"), pigtest.Text("on"), pigtest.Text("on again"), pigtest.Text("disabled"), pigtest.Text("enabled again"))
	defer mock.Close()
	probe := probePath(t)
	home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{probe}, Prompts: []string{"baseline"}})
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t), probe}, Prompts: []string{
		"default off", "/adhd status", "/adhd on", "/adhd on", "/adhd status", "first enabled turn", "second enabled turn", "/adhd off", "/adhd off", "disabled turn", "/adhd on", "enabled again",
	}})
	assertNoErrors(t, result)
	requests := mock.Requests()
	if len(requests) != 6 {
		t.Fatalf("commands triggered model calls: got %d, want 6", len(requests))
	}
	base := systemText(t, requests[0])
	for _, sentinel := range []string{"<tools>", "UNCHANGED_APPEND_SYSTEM_SENTINEL", "UNCHANGED_PROJECT_INSTRUCTIONS_SENTINEL", "UNCHANGED_EXTENSION_SENTINEL"} {
		if !strings.Contains(base, sentinel) {
			t.Fatalf("baseline lacks %q", sentinel)
		}
	}
	for i, request := range requests[1:] {
		if systemText(t, request) != base {
			t.Fatalf("turn %d replaced or changed the base/system prompt", i+1)
		}
		if !reflect.DeepEqual(request["tools"], requests[0]["tools"]) {
			t.Fatalf("turn %d changed model-callable tools", i+1)
		}
	}
	if strings.Contains(requestText(t, requests[1]), mode.RulesType) {
		t.Fatal("default off injected rules")
	}
	for _, i := range []int{2, 3} {
		text := requestText(t, requests[i])
		if strings.Count(text, "["+mode.RulesType+" sha256=") != 1 {
			t.Fatalf("turn %d lacks exactly one complete production rules marker", i)
		}
		if !strings.Contains(text, "These rules control presentation only.") {
			t.Fatal("safety qualification missing from provider context")
		}
	}
	text := requestText(t, requests[4])
	if strings.Count(text, "["+mode.RulesType+" sha256=") != 1 || strings.Count(text, "["+mode.DisabledType+"]") != 1 || strings.LastIndex(text, mode.DisabledType) < strings.LastIndex(text, mode.RulesType) {
		t.Fatal("off provider context did not cancel unavoidable historical instructions")
	}
	if strings.Count(requestText(t, requests[5]), "["+mode.RulesType+" sha256=") != 2 {
		t.Fatal("on after off did not activate one new copy")
	}
	if !strings.Contains(strings.Join(result.Statuses(mode.StatusKey), "\n"), mode.Badge) {
		t.Fatal("host status API did not receive the ADHD badge")
	}
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("user prompt file changed: %s: %v", path, err)
		}
	}
	// Report the exact added body size. The token count is the host's character-based
	// estimate, not a claim about a provider's tokenizer or scripted mock usage.
	rules := mode.RulesMessage(defaultRules)
	t.Logf("rules body: %d bytes, %d Unicode characters, approximately %d prompt tokens (PiG character/4 estimate)", len(rules), len([]rune(rules)), (len([]rune(rules))+3)/4)
}

func TestProviderBoundResumeBranchReloadNewSession(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t), probePath(t)}, Prompts: []string{
		"/adhd off", "off branch", "/adhd on", "on branch", "/adhd-test-branch off", "restored off branch", "/adhd-test-branch on", "restored on branch", "/adhd-test-reload", "reloaded on branch", "/adhd status",
	}})
	assertNoErrors(t, result)
	requests := mock.Requests()
	if len(requests) != 5 {
		t.Fatalf("branch/reload commands started model turns: %d", len(requests))
	}
	if strings.Contains(requestText(t, requests[2]), mode.RulesType) {
		t.Fatal("inactive on branch leaked into earlier off branch")
	}
	if strings.Count(requestText(t, requests[3]), "["+mode.RulesType+" sha256=") != 1 {
		t.Fatal("returning to on branch lost or duplicated rules")
	}
	if strings.Count(requestText(t, requests[4]), "["+mode.RulesType+" sha256=") != 1 {
		t.Fatal("actual reload duplicated rules")
	}
	// A new host process is also a new extension instance: resume must reconstruct
	// state from session records, not from the previous factory's process memory.
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	resumed := home.RunPig(t, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "resumed turn"}, "-p", "--continue", "--provider", "mock", "--model", "mock-model")
	if resumed.ExitCode != 0 {
		t.Fatalf("resume failed: %s\n%s", resumed.Stdout, resumed.Stderr)
	}
	requests = mock.Requests()
	if len(requests) != 6 || strings.Count(requestText(t, requests[5]), "["+mode.RulesType+" sha256=") != 1 {
		t.Fatal("resume/reloaded instance duplicated rules or lost on state")
	}
	result = home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t), probePath(t)}, Prompts: []string{"/adhd on", "enabled new host", "/adhd-test-new", "fresh off session"}})
	assertNoErrors(t, result)
	requests = mock.Requests()
	if len(requests) != 8 || strings.Contains(requestText(t, requests[7]), mode.RulesType) {
		t.Fatal("new session inherited earlier toggle")
	}
}

func TestProviderBoundSuccessfulAndAbortedCompaction(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	settings := filepath.Join(home.AgentDir(), "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"compaction":{"enabled":true,"keepRecentTokens":1,"reserveTokens":100}}`), 0600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t), probePath(t)}, Prompts: []string{
		"old history " + strings.Repeat("previous context ", 200), "/adhd on", "before compact", "/adhd-test-compact removed", "after removed compact", "/adhd-test-compact retained", "after retained compact", "/adhd-test-compact cancel", "after cancelled compact", "/adhd off", "/adhd-test-compact removed", "disabled after compact",
	}})
	assertNoErrors(t, result)
	requests := mock.Requests()
	if len(requests) != 6 {
		t.Fatalf("toggles/compaction restoration made extra model calls: %d", len(requests))
	}
	for _, i := range []int{2, 3, 4} {
		text := requestText(t, requests[i])
		if !strings.Contains(text, "ADHD_TEST_COMPACT_SUMMARY") || strings.Count(text, "["+mode.RulesType+" sha256=") != 1 {
			t.Fatalf("post-compact turn %d has absent/duplicate rules: markers=%d, summary=%v", i, strings.Count(text, "["+mode.RulesType+" sha256="), strings.Contains(text, "ADHD_TEST_COMPACT_SUMMARY"))
		}
	}
	if strings.Contains(requestText(t, requests[5]), mode.RulesType) {
		t.Fatal("disabled post-compact context received rules")
	}
}

func TestPrintModeFlagWithoutTUI(t *testing.T) {
	pigtest.RequirePig(t)
	for _, test := range []struct {
		name      string
		flags     []string
		wantRules int
	}{
		{name: "bare", flags: []string{"--adhd"}, wantRules: 1},
		{name: "explicit true", flags: []string{"--adhd", "true"}, wantRules: 1},
		{name: "explicit false", flags: []string{"--adhd", "false"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := pigtest.NewHome(t)
			mock := pigtest.NewMockLLM()
			defer mock.Close()
			home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
			flags := append([]string{"-p", "--no-session", "--provider", "mock", "--model", "mock-model"}, test.flags...)
			result := home.RunPig(t, pigtest.RunOptions{Extensions: []string{extensionPath(t)}, Prompt: "first turn"}, flags...)
			if result.ExitCode != 0 {
				t.Fatalf("print mode: %s\n%s", result.Stdout, result.Stderr)
			}
			requests := mock.Requests()
			if len(requests) != 1 || strings.Count(requestText(t, requests[0]), "["+mode.RulesType+" sha256=") != test.wantRules {
				t.Fatal("launch flag did not reach first provider request without TUI")
			}
		})
	}
}

func TestProviderBoundForkRetainsInheritedChoice(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t), probePath(t)}, Prompts: []string{
		"/adhd off", "first off user message", "/adhd on", "later enabled message", "/adhd-test-fork", "forked off turn",
	}})
	assertNoErrors(t, result)
	requests := mock.Requests()
	if len(requests) != 3 || strings.Contains(requestText(t, requests[2]), mode.RulesType) {
		t.Fatal("fork did not retain earlier off ancestry")
	}
}

func TestProviderBoundFailedCompactionDoesNotRestore(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	settings := filepath.Join(home.AgentDir(), "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"compaction":{"enabled":true,"keepRecentTokens":1,"reserveTokens":100}}`), 0600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Text("old history reply"), pigtest.Text("before failure reply"), pigtest.HTTPError(400, "requested test compaction failure"), pigtest.Text("after failure reply"))
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionPath(t), probePath(t)}, Prompts: []string{
		"old history " + strings.Repeat("history ", 200), "/adhd on", "before failed compact", "/adhd-test-compact fail", "after failed compact",
	}})
	requests := mock.Requests()
	// One provider call is the requested summarization attempt; the extension
	// must not add a restoration/model call when that attempt fails.
	if len(requests) != 4 {
		t.Fatalf("failed compaction made unexpected provider calls: %d, want 4", len(requests))
	}
	text := requestText(t, requests[3])
	if strings.Count(text, "["+mode.RulesType+" sha256=") != 1 || !strings.Contains(text, "old history reply") || strings.Contains(text, "ADHD_TEST_COMPACT_SUMMARY") {
		t.Fatal("failed compaction changed the previous context or duplicated rules")
	}
	data, err := json.Marshal(result.Events)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "requested test compaction failure") {
		t.Fatal("compaction failure was not observable")
	}
}
