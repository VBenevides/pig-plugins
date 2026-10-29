package compactsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/compactlimit"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// The mock model's context window is 100000 and the default reserve is 16384, so PiG's own threshold is 83616.
const nativeThreshold = 100000 - 16384

func writeSettings(t *testing.T, home *pigtest.Home, reserve string) {
	t.Helper()
	dir := home.AgentDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	text := `{"compaction":{"enabled":true,"keepRecentTokens":1` + reserve + `}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func extensionDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, replies []pigtest.Reply, prompts ...string) (pigtest.RPCResult, int) {
	t.Helper()
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(replies...)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: prompts})
	for _, event := range result.Events {
		if event["type"] == "extension_error" {
			t.Fatalf("extension failed: %v", event)
		}
	}
	return result, len(mock.Requests())
}

func TestAbsoluteLimitCompactsBelowPigsOwnThreshold(t *testing.T) {
	usage := nativeThreshold - 10000 // Above a 50k limit, below PiG's own trigger.
	replies := []pigtest.Reply{{Text: "first", PromptTokens: usage}}

	_, without := run(t, replies, "first prompt "+strings.Repeat("history ", 200), "second prompt")
	if without != 2 {
		t.Fatalf("without a limit PiG must not compact below its threshold: %d model requests, want 2", without)
	}
	result, with := run(t, replies, "/compact-settings limit absolute 50k", "first prompt "+strings.Repeat("history ", 200), "second prompt")
	if with != 3 {
		t.Fatalf("a 50k limit must add one summary request: %d model requests, want 3\nnotices: %q\nstderr: %s", with, result.Notices(), result.Stderr)
	}
}

func TestLimitAboveContextOrCapDoesNotCompact(t *testing.T) {
	replies := []pigtest.Reply{{Text: "first", PromptTokens: 40000}}
	_, requests := run(t, replies, "/compact-settings limit absolute 50k", "/compact-settings limit relative 90", "first prompt", "second prompt")
	if requests != 2 {
		t.Fatalf("40k context is below both limits; %d model requests, want 2", requests)
	}
}

func TestRelativeLimitUsesThePercentageOfTheWindow(t *testing.T) {
	replies := []pigtest.Reply{{Text: "first", PromptTokens: 30000}}
	_, requests := run(t, replies, "/compact-settings limit relative 25", "first prompt "+strings.Repeat("history ", 200), "second prompt")
	if requests != 3 {
		t.Fatalf("30k is above 25%% of 100k; %d model requests, want 3", requests)
	}
}

func TestCommandsValidatePersistAndReport(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{extensionDir(t)}, Prompts: []string{
		"/compact-settings limit absolute 200k",
		"/compact-settings limit absolute 2M",
		"/compact-settings limit relative 150",
		"/compact-settings limit absolute 1.0005k",
		"/compact-settings limit relative 40",
		"/compact-settings limit bogus",
		"/compact-settings limit absolute off",
		"/compact-settings",
		"/compact-settings limit off",
	}})
	notices := result.Notices()
	if len(notices) != 9 {
		t.Fatalf("notices = %q\nstderr: %s", notices, result.Stderr)
	}
	for _, want := range []struct {
		index int
		text  string
	}{
		{1, "clamped to the cap"},
		{2, "above 100"},
		{3, "whole number of tokens"},
		{4, "limit relative: 40% of the context window"},
		{5, "usage:"},
		{7, "limit absolute: off"},
		{8, "limit relative: off"},
	} {
		if !strings.Contains(notices[want.index], want.text) {
			t.Errorf("notice %d lacks %q: %s", want.index, want.text, notices[want.index])
		}
	}
	// A rejected command left the earlier value, and the file holds what the last accepted command wrote.
	file := filepath.Join(home.AgentDir(), compactlimit.FileName)
	limits, err := compactlimit.Load(file)
	if err != nil || limits.IsSet() {
		t.Fatalf("after 'limit off' the file must hold no limit: %+v %v", limits, err)
	}
	if !strings.Contains(notices[0], "limit absolute: 200k tokens (200000)") || !strings.Contains(notices[0], "effective limit: 84k (83616)") {
		t.Errorf("the 200k absolute limit must report the 83.6k cap: %s", notices[0])
	}
}

// Set the limit only after the first run, so the settled hook cannot compact it.
// Completion within RunRPC's timeout also detects a circular wait on compact events.
func TestIdleInputCompactsBeforeNextModelRequest(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Text: "first", PromptTokens: 60000},
		pigtest.Text("saved summary"),
		pigtest.Text("second answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"first prompt " + strings.Repeat("history ", 200), "/compact-settings limit absolute 50k", "unique-next-question"},
	})
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("want initial, summary, answer requests; got %d; notices: %q; stderr: %s", len(requests), result.Notices(), result.Stderr)
	}
	for i, request := range requests {
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(data), "unique-next-question"); got != (i == 2) {
			t.Errorf("request %d includes next question = %v; want %v", i, got, i == 2)
		}
	}
}

func TestIdleInputContinuesAfterCompactionFailure(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Text: "first", PromptTokens: 60000},
		pigtest.HTTPError(400, "summary deliberately rejected"),
		pigtest.Text("answer after failed summary"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"first prompt " + strings.Repeat("history ", 200), "/compact-settings limit absolute 50k", "next question"},
	})
	if len(mock.Requests()) != 3 {
		t.Fatalf("want initial, failed summary, answer requests; got %d; stderr: %s", len(mock.Requests()), result.Stderr)
	}
	if !strings.Contains(strings.Join(result.Notices(), "\n"), "compacting at 60k tokens failed") {
		t.Fatalf("missing compaction failure notice: %q", result.Notices())
	}
}

func requestText(t *testing.T, request map[string]any) string {
	t.Helper()
	data, err := json.Marshal(request["messages"])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func finalAssistantText(events []map[string]any, want string) bool {
	for _, event := range events {
		if event["type"] != "message_end" {
			continue
		}
		data, err := json.Marshal(event["message"])
		if err == nil && strings.Contains(string(data), `"role":"assistant"`) && strings.Contains(string(data), want) {
			return true
		}
	}
	return false
}

// A run that passes the limit after a tool result must compact before the next model request, then continue.
func TestTurnBoundaryCompactsInsideRunAndResumes(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("bash", map[string]any{"command": "echo tool-output"})}, PromptTokens: 60000},
		pigtest.Text("saved summary"),
		pigtest.Text("final answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	requests := mock.Requests()
	if len(requests) != 3 {
		t.Fatalf("want task, summary, resumed requests; got %d; notices: %q; stderr: %s", len(requests), result.Notices(), result.Stderr)
	}
	if !strings.Contains(requestText(t, requests[1]), "ONLY output the structured summary") {
		t.Errorf("the second request must be the compaction summary request: %s", requestText(t, requests[1]))
	}
	if !strings.Contains(requestText(t, requests[2]), "saved summary") {
		t.Errorf("the resumed request must carry the summary: %s", requestText(t, requests[2]))
	}
	if !finalAssistantText(result.Events, "final answer") {
		t.Errorf("the task did not resume after compaction; stderr: %s", result.Stderr)
	}
}

// A compaction that fails mid-run must not stop the run or retry at the same size.
func TestTurnBoundaryCompactionFailureLetsTheRunContinue(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("bash", map[string]any{"command": "echo tool-output"})}, PromptTokens: 60000},
		pigtest.HTTPError(400, "summary deliberately rejected"),
		pigtest.Text("final answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	if !finalAssistantText(result.Events, "final answer") {
		t.Fatalf("the run must continue after a failed compaction; notices: %q; stderr: %s", result.Notices(), result.Stderr)
	}
	if !strings.Contains(strings.Join(result.Notices(), "\n"), "compacting at 60k tokens failed") {
		t.Errorf("missing compaction failure notice: %q", result.Notices())
	}
}

// A compact cannot shrink a context whose floor is above the limit, so it must not repeat on every turn.
func TestTurnBoundaryCompactionDoesNotRepeatWhileContextStaysAboveTheLimit(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	call := pigtest.Call("bash", map[string]any{"command": "echo tool-output"})
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Calls: []pigtest.ToolCall{call}, PromptTokens: 60000},
		pigtest.Text("saved summary"),
		pigtest.Reply{Calls: []pigtest.ToolCall{call}, PromptTokens: 60000},
		pigtest.Reply{Calls: []pigtest.ToolCall{call}, PromptTokens: 61000},
		pigtest.Text("final answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	summaries := 0
	for _, request := range mock.Requests() {
		if strings.Contains(requestText(t, request), "ONLY output the structured summary") {
			summaries++
		}
	}
	if summaries != 1 || !finalAssistantText(result.Events, "final answer") {
		t.Fatalf("want one summary request and a finished task; got %d summaries of %d requests; stderr: %s", summaries, len(mock.Requests()), result.Stderr)
	}
}

// A compact can fail for a reason that passes, such as a usage limit. The next prompt is a new attempt, even
// though the context has the same size.
func TestIdleInputRetriesACompactionThatFailedAtTheSameSize(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Text: "first", PromptTokens: 60000},
		pigtest.HTTPError(400, "summary deliberately rejected"),
		pigtest.Text("saved summary"),
		pigtest.Text("second answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "first prompt " + strings.Repeat("history ", 200), "unique-next-question"},
	})
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("want task, failed summary, retried summary, answer; got %d; notices: %q; stderr: %s", len(requests), result.Notices(), result.Stderr)
	}
	if !strings.Contains(requestText(t, requests[2]), "ONLY output the structured summary") || strings.Contains(requestText(t, requests[2]), "unique-next-question") {
		t.Errorf("the third request must be the retried summary: %s", requestText(t, requests[2]))
	}
}

// An assistant error carries no usage. The size of the earlier valid reply must still trigger the compact.
func TestIdleInputCompactsAfterAnAssistantError(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Text: "first", PromptTokens: 60000},
		pigtest.HTTPError(400, "request rejected"),
		pigtest.Text("saved summary"),
		pigtest.Text("second answer"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"first prompt " + strings.Repeat("history ", 200), "failing prompt", "/compact-settings limit absolute 50k", "unique-next-question"},
	})
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("want task, error, summary, answer; got %d; notices: %q; stderr: %s", len(requests), result.Notices(), result.Stderr)
	}
	if !strings.Contains(requestText(t, requests[2]), "ONLY output the structured summary") {
		t.Errorf("the third request must be the summary: %s", requestText(t, requests[2]))
	}
}

func bigRunMock() *pigtest.MockLLM {
	read := func(path string, tokens int) pigtest.Reply {
		return pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": path})}, PromptTokens: tokens}
	}
	return pigtest.NewMockLLM(
		read("big.txt", 10), read("big.txt", 10), read("a.txt", 10), read("b.txt", 10), read("a.txt", 10), read("b.txt", 10), read("a.txt", 10), read("b.txt", 10),
		read("a.txt", 10), read("b.txt", 10), read("a.txt", 10), read("b.txt", 60000),
		pigtest.Text("saved summary"),
		pigtest.Text("final answer"),
	)
}

func writeBigRunFiles(t *testing.T, home *pigtest.Home) {
	t.Helper()
	files := map[string]string{"big.txt": strings.Repeat(strings.Repeat("x", 99)+"\n", 1000), "a.txt": "a\n", "b.txt": "b\n"}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(home.Work, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func countSummaryRequests(t *testing.T, mock *pigtest.MockLLM) int {
	t.Helper()
	count := 0
	for _, request := range mock.Requests() {
		if strings.Contains(requestText(t, request), "ONLY output the structured summary") {
			count++
		}
	}
	return count
}

func pruneExtensionDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../context-prune")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// When pruning alone brings the context under the limit, the run continues with no summary request.
func TestPruningThatRelievesThePressureSkipsTheSummary(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	writeBigRunFiles(t, home)
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := bigRunMock()
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t), pruneExtensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	if got := countSummaryRequests(t, mock); got != 0 {
		t.Fatalf("pruning relieves the pressure, but %d summary requests ran; notices %q; stderr %s", got, result.Notices(), result.Stderr)
	}
	requests := mock.Requests()
	if len(requests) != 13 || !strings.Contains(requestText(t, requests[12]), "[pruned:") {
		t.Fatalf("the request after the pressure must be pruned; got %d requests; stderr %s", len(requests), result.Stderr)
	}
}

// Without pruning the same run needs a summary.
func TestSamePressureNeedsASummaryWhenPruningIsOff(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	writeBigRunFiles(t, home)
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := bigRunMock()
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t), pruneExtensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	if got := countSummaryRequests(t, mock); got != 1 {
		t.Fatalf("want one summary request, got %d", got)
	}
}

// Pruning that cannot remove enough does not replace the summary.
func TestPruningThatCannotRelieveThePressureStillSummarizes(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(
		pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("bash", map[string]any{"command": "echo tool-output"})}, PromptTokens: 60000},
		pigtest.Text("saved summary"),
		pigtest.Text("final answer"),
	)
	defer mock.Close()
	home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t), pruneExtensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	if got := countSummaryRequests(t, mock); got != 1 {
		t.Fatalf("want one summary request, got %d", got)
	}
}

// When the pruner does not lower the context, the next turn end summarizes instead of waiting again.
func TestMidRunReliefThatDoesNotHappenFallsBackToASummary(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	writeSettings(t, home, "")
	writeBigRunFiles(t, home)
	if err := os.WriteFile(filepath.Join(home.AgentDir(), "context-prune.json"), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(path string, tokens int) pigtest.Reply {
		return pigtest.Reply{Calls: []pigtest.ToolCall{pigtest.Call("read", map[string]any{"path": path})}, PromptTokens: tokens}
	}
	mock := pigtest.NewMockLLM(
		read("big.txt", 10), read("big.txt", 10), read("a.txt", 10), read("b.txt", 10), read("a.txt", 10), read("b.txt", 10), read("a.txt", 10),
		read("b.txt", 10), read("a.txt", 10), read("b.txt", 10), read("a.txt", 10),
		read("b.txt", 60000), read("a.txt", 60000),
		pigtest.Text("saved summary"),
		pigtest.Text("final answer"),
	)
	defer mock.Close()
	// Only compact-settings is loaded, so nothing applies the pruning that it counted on.
	home.RunRPC(t, mock, pigtest.RPCOptions{
		Extensions: []string{extensionDir(t)},
		Prompts:    []string{"/compact-settings limit absolute 50k", "unique-task-prompt"},
	})
	if got := countSummaryRequests(t, mock); got != 1 {
		t.Fatalf("want one summary request after the relief failed, got %d", got)
	}
}
