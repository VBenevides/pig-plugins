package compactsettings

import (
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
