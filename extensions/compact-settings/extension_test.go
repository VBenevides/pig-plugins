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
