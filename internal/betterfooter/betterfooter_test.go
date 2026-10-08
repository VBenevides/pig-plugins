package betterfooter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionStatsAppendReplaceAndSideUsage(t *testing.T) {
	entries := []json.RawMessage{json.RawMessage(`{"type":"message","message":{"role":"assistant","usage":{"input":10,"output":5,"cacheRead":30,"cacheWrite":10,"cost":{"total":0.2}}}}`), json.RawMessage(`{"type":"usage","usage":{"input":3,"output":7,"cost":{"total":0.1}}}`), json.RawMessage(`{"type":"message","message":{"role":"toolResult","usage":{"output":2}}}`)}
	var stats SessionStats
	totals, hit, known := stats.Summarize(entries)
	if totals.Input != 13 || totals.Output != 14 || !known || hit != 60 || totals.Cost < 0.299 {
		t.Fatalf("stats %+v hit %v", totals, hit)
	}
	again, _, _ := stats.Summarize(entries)
	if again != totals {
		t.Fatal("double counted unchanged entries")
	}
	entries = append(entries, json.RawMessage(`{"type":"compaction","usage":{"output":4}}`))
	totals, _, _ = stats.Summarize(entries)
	if totals.Output != 18 {
		t.Fatalf("append: %+v", totals)
	}
	totals, _, known = stats.Summarize([]json.RawMessage{json.RawMessage(`{"type":"branch_summary","usage":{"input":1}}`)})
	if totals.Input != 1 || totals.Output != 0 || known {
		t.Fatalf("replacement retained totals: %+v", totals)
	}
}
func TestGuardStatusesUseThirdRow(t *testing.T) {
	state := RenderState{Cwd: "/p", Statuses: map[string]string{"auto-model": "am", "smart-approve-lancet": "sal", "pi-curator": "pc"}}
	lines := RenderFooter(state, 120, Theme{}, time.Unix(1000, 0))
	if len(lines) != 3 || !strings.Contains(lines[0], "am") || strings.Contains(lines[0], "sal") || !strings.Contains(lines[2], "[sal][pc]") {
		t.Fatalf("rows = %q", lines)
	}
	state.Statuses = map[string]string{"auto-model": "am"}
	if got := len(RenderFooter(state, 120, Theme{}, time.Unix(1000, 0))); got != 2 {
		t.Fatalf("empty third row must be omitted, got %d rows", got)
	}
}

func TestRendererFieldsStatusesAndWidths(t *testing.T) {
	now := time.Unix(1000, 0)
	tokens := 5000
	state := RenderState{Cwd: "/home/u/project", Home: "/home/u/", Branch: "main", Version: "v1.2.3", Git: GitChanges{Added: 2, Removed: 1, Dirty: true}, Provider: "test", Model: "model", Reasoning: true, Thinking: "high", ContextTokens: &tokens, ContextWindow: 200000, Speed: 99, Estimated: true, Totals: Totals{Input: 1000, Output: 20, CacheRead: 500, Cost: 0.123}, LatestHit: 33.3, HasHit: true, Quota: ProviderQuota{Windows: []RateWindow{{Scope: "weekly", Percent: 25, HasReset: true, ResetSec: 3600, CapturedAt: now}}}, Statuses: map[string]string{"z": "last\nline", "a": "first"}}
	lines := RenderFooter(state, 180, Theme{}, now)
	for _, want := range []string{"~/project", "main", "v1.2.3", "+2 -1", "first  last line", "↑1.0k/500", "↓20", "CH33.3%", "$0.123", "5.0k/200k", "~99t/s", "test/model high", "1h 25%"} {
		if !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Fatalf("missing %q: %v", want, lines)
		}
	}
	for width := range 181 {
		for _, line := range RenderFooter(state, width, Theme{}, now) {
			if VisibleWidth(line) > width {
				t.Fatalf("overflow width=%d %q", width, line)
			}
		}
	}
	if strings.Contains(RenderFooter(state, 65, Theme{}, now)[1], "t/s") {
		t.Fatal("speed was not dropped at narrow width")
	}
}
func TestLightThemeWarningsAndExpiredWindows(t *testing.T) {
	now := time.Unix(1000, 0)
	theme := Theme{Appearance: "light", Fg: func(color, text string) string { return "[" + color + "]" + text }}
	s := RenderState{ContextWindow: 100, ContextPercent: 80, Speed: 60, Quota: ProviderQuota{Windows: []RateWindow{{Percent: 25}, {Percent: 7, HasReset: true, ResetSec: 1, CapturedAt: now.Add(-2 * time.Second)}}}}
	output := strings.Join(RenderFooter(s, 250, theme, now), "\n")
	if !strings.Contains(output, "[syntaxFunction]") || strings.Contains(output, "7%") {
		t.Fatalf("light or expiry: %s", output)
	}
	s.QuotaKey = ChatGPTQuotaKey
	s.Quota.ChatGPTLimitAt = now
	output = strings.Join(RenderFooter(s, 250, Theme{}, now), "\n")
	if !strings.Contains(output, "ChatGPT limit") || !strings.Contains(output, ChatGPTUsageURL) {
		t.Fatal(output)
	}
}
func TestRecentPersistenceAndCLIOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent", "state.json")
	ref := ModelRef{"p", "m"}
	if _, err := SaveRecentState(path, ref, "high", 20); err != nil {
		t.Fatal(err)
	}
	if changed, err := SaveRecentState(path, ModelRef{"old", "old"}, "low", 10); err != nil || changed {
		t.Fatalf("newer state overwritten: %v %v", changed, err)
	}
	state, found, err := LoadRecentState(path)
	if err != nil || !found || state.Ref() != ref || state.ThinkingLevel != "high" {
		t.Fatalf("state %+v %v", state, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("state permissions")
	}
	for _, args := range [][]string{{"--model=p/m"}, {"--provider", "p"}} {
		if ShouldRestore("startup", args, false, false) {
			t.Fatal("explicit selection lost")
		}
	}
	if ShouldRestore("startup", nil, true, false) || ShouldRestore("resume", nil, false, false) || ShouldRestore("new", nil, false, true) {
		t.Fatal("resumed or disabled restore")
	}
	if !ShouldRestore("new", []string{"--model", "p/m"}, true, false) {
		t.Fatal("new session should restore")
	}
	if got := RestoreThinkingLevel("startup", []string{"--thinking=low"}, true, "medium", "high"); got != "low" {
		t.Fatal(got)
	}
	if value, ok := StartupOption([]string{"--thinking=high", "--thinking", "low", "--", "--thinking=max"}, "--thinking"); !ok || value != "low" {
		t.Fatalf("args %q %v", value, ok)
	}
	if err := os.WriteFile(path, []byte(`{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecentState(path, ref, "high", 30); err == nil {
		t.Fatal("corrupt user state silently overwritten")
	}
}
func TestSettingsDefaultsAndAtomicToggle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := LoadSettings(path)
	if err != nil || settings != DefaultSettings() {
		t.Fatal(settings, err)
	}
	if err := os.WriteFile(path, []byte(`{"keepRecentModel":false,"skipExhaustedScopedModels":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadSettings(path)
	if err != nil || settings.KeepRecentModel || !settings.SkipExhaustedScopedModels {
		t.Fatal(settings, err)
	}
	next, ok := settings.Toggle("skipExhaustedScopedModels")
	if !ok || next.SkipExhaustedScopedModels {
		t.Fatal(next)
	}
	if err := SaveSettings(path, next); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadSettings(path)
	if err != nil || saved != next {
		t.Fatal(saved, err)
	}
}
func TestExhaustionAndQuotaIdentity(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		window    RateWindow
		exhausted bool
	}{{RateWindow{Percent: 0, CapturedAt: now}, true}, {RateWindow{Percent: 0, CapturedAt: now.Add(-6 * time.Minute)}, false}, {RateWindow{Percent: 0, Advisory: true, CapturedAt: now}, false}, {RateWindow{Percent: 0, HasReset: true, ResetSec: 0, CapturedAt: now}, false}, {RateWindow{Percent: 0, HasReset: true, ResetSec: 60, CapturedAt: now}, true}} {
		if got := IsQuotaExhausted(ProviderQuota{Windows: []RateWindow{tc.window}}, true, now); got != tc.exhausted {
			t.Fatalf("window %+v got %v", tc.window, got)
		}
	}
	if QuotaKey("pi-virtual", "openai", "https://api.openai.com", func() bool { return true }) != "" || QuotaKey("responses", "openai", "https://api.openai.com", func() bool { return true }) != ChatGPTQuotaKey || QuotaKey("responses", "openai", "https://proxy.example", func() bool { return true }) != "openai" {
		t.Fatal("quota account isolation")
	}
	if ZaiQuotaURL([]string{"https://proxy.example"}) != "" || UsesOpenCodeHost([]string{"https://opencode.ai.evil.test"}) {
		t.Fatal("proxy key would leak")
	}
	refs := []ModelRef{{"a", "1"}, {"b", "2"}, {"c", "3"}}
	if CycleDirection(refs, refs[0], refs[2]) != -1 || CycleDirection(refs, refs[2], refs[0]) != 1 {
		t.Fatal("cycle direction")
	}
}
func TestQuotaSourceParsing(t *testing.T) {
	now := time.Unix(1000, 0)
	zai, err := ParseZaiQuota([]byte(`{"data":{"limits":[{"type":"CREDIT_LIMIT","unit":3,"percentage":80,"remaining":999},{"type":"TIME_LIMIT","unit":5,"usage":1000,"remaining":975}]}}`), now)
	if err != nil || len(zai) != 2 || zai[0].Percent != 20 || zai[1].Percent != 97.5 || !zai[1].Advisory {
		t.Fatalf("zai %v %v", zai, err)
	}
	codex, err := ParseCodexRateLimits(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":100},"individualLimit":{"remainingPercent":0},"credits":{"hasCredits":true}}}`), now)
	if err != nil || len(codex) != 2 || !codex[0].Advisory || codex[1].Advisory {
		t.Fatalf("codex %v %v", codex, err)
	}
	open, err := ParseOpenCodeGoUsage([]byte(`{"usage":{"rolling":{"percent":40,"resetsAt":"1970-01-01T01:00:00Z"},"monthly":{"percent":10}}}`), now)
	if err != nil || len(open) != 2 || open[0].Percent != 60 || open[1].Percent != 90 {
		t.Fatal(open, err)
	}
	dash := ParseOpenCodeGoDashboard(`rollingUsage:$R[3]={usagePercent:17.5,resetInSec:2345.6}`, now)
	if len(dash) != 1 || dash[0].Percent != 82.5 || dash[0].ResetSec != 2346 {
		t.Fatal(dash)
	}
	credits, ok := ParseCopilotCredits([]byte(`{"quota_snapshots":{"premium_interactions":{"remaining":"5","entitlement":10}}}`))
	if !ok || credits != "5/10" {
		t.Fatal(credits)
	}
}
func TestHeadersPreferTokensAndKeepCodexCountdown(t *testing.T) {
	now := time.Unix(1000, 0)
	headers := map[string]string{"X-RateLimit-Limit-Tokens": "100", "x-ratelimit-remaining-tokens": "20", "x-ratelimit-reset-tokens": "0s", "x-ratelimit-limit-requests": "10", "x-ratelimit-remaining-requests": "0"}
	windows := ToRateWindows(DetectRateWindows(headers), now)
	if len(windows) != 1 || windows[0].Scope != "tokens" || windows[0].Percent != 20 || !windows[0].HasReset || windows[0].Active(now) {
		t.Fatal(windows)
	}
	previous := RateWindow{Scope: "codex:primary", Percent: 50, HasReset: true, ResetSec: 60, CapturedAt: now, Advisory: true}
	updated := ParseCodexUsageHeaders(map[string]string{"x-codex-primary-used-percent": "80"}, 200, []RateWindow{previous}, now.Add(10*time.Second))
	if len(updated) != 1 || updated[0].Remaining(now.Add(10*time.Second)) != 50 || !updated[0].Advisory {
		t.Fatal(updated)
	}
	w, ok := ParseLimitError("Usage limit reached for 5 hour. Your limit will reset at 1970-01-02 08:00:00", now)
	if !ok || w.Scope != "zai:3" || w.ResetSec != 85400 {
		t.Fatal(w, ok)
	}
}
func TestHTTPRedirectBodyCapAndSafeErrors(t *testing.T) {
	var destination atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destination.Add(1) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, other.URL, 302)
		case "/large":
			w.Write([]byte(strings.Repeat("a", MaxResponseBytes+1)))
		default:
			w.WriteHeader(401)
			w.Write([]byte("secret-token"))
		}
	}))
	defer server.Close()
	for _, path := range []string{"/redirect", "/large", "/denied"} {
		_, err := httpGet(context.Background(), nil, "test", server.URL+path, map[string]string{"Authorization": "Bearer secret-token"})
		if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), server.URL) {
			t.Fatalf("unsafe or absent error: %v", err)
		}
	}
	if destination.Load() != 0 {
		t.Fatal("credential redirect followed")
	}
}
func TestQuotaReadCannotOverwriteNewerHeaders(t *testing.T) {
	var store QuotaStore
	started, release := make(chan struct{}), make(chan struct{})
	reader := Reader{Store: &store, Codex: func(context.Context) ([]RateWindow, error) {
		close(started)
		<-release
		return []RateWindow{{Scope: "old", Percent: 5}}, nil
	}}
	done := make(chan struct{})
	go func() { defer close(done); reader.Read(context.Background(), CodexProvider, false, 0) }()
	<-started
	store.Update(CodexProvider, time.Now(), func(q *ProviderQuota) { q.Windows = []RateWindow{{Scope: "new", Percent: 50}} })
	close(release)
	<-done
	q, _ := store.Get(CodexProvider)
	if q.Windows[0].Scope != "new" {
		t.Fatal(q)
	}
	reader.Codex = func(context.Context) ([]RateWindow, error) { return nil, errors.New("unavailable") }
	q, known, err := reader.Read(context.Background(), CodexProvider, false, 0)
	if err == nil || !known || q.Windows[0].Scope != "new" {
		t.Fatal("failed refresh discarded valid neighbor", q, err)
	}
}
func TestGitCountsUntrackedWithoutTouchingIndex(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "initial")
	before, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("a\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, ok, err := ReadGitChanges(context.Background(), dir)
	if err != nil || !ok || !changes.Dirty || changes.Added != 3 || changes.Removed != 1 {
		t.Fatal(changes, ok, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil || string(before) != string(after) {
		t.Fatal("real index modified", err)
	}
}

func TestDecodedSessionEntriesRemainIncremental(t *testing.T) {
	var stats SessionStats
	entries := []map[string]any{{"id": "a", "type": "message", "message": map[string]any{"role": "assistant", "usage": map[string]any{"input": float64(10), "cacheRead": float64(30), "output": float64(5)}}}}
	totals, hit, known := stats.SummarizeEntries(entries)
	if totals.Input != 10 || totals.Output != 5 || hit != 75 || !known {
		t.Fatal(totals, hit, known)
	}
	again, _, _ := stats.SummarizeEntries(entries)
	if again != totals {
		t.Fatal("decoded entries counted twice")
	}
	entries = append(entries, map[string]any{"id": "b", "type": "usage", "usage": map[string]any{"output": float64(7)}})
	totals, _, _ = stats.SummarizeEntries(entries)
	if totals.Output != 12 {
		t.Fatal(totals)
	}
	totals, _, known = stats.SummarizeEntries([]map[string]any{{"id": "replacement", "type": "usage", "usage": map[string]any{"input": float64(1)}}})
	if totals.Input != 1 || totals.Output != 0 || known {
		t.Fatal("session replacement retained decoded totals", totals)
	}
}

func TestMetadataCannotInjectTerminalCommands(t *testing.T) {
	state := RenderState{Cwd: "/work/\x1b]52;c;secret\x07project", Branch: "main", Provider: "p", Model: "m\x1b[2J"}
	for _, line := range RenderFooter(state, 100, Theme{}, time.Now()) {
		if strings.ContainsRune(line, '\x1b') || strings.Contains(line, "secret") {
			t.Fatalf("terminal command escaped metadata: %q", line)
		}
	}
}

func TestStateReadsAreBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", MaxResponseBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(path); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatal("unbounded settings read")
	}
	if _, _, err := LoadRecentState(path); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatal("unbounded recent-state read")
	}
}

func TestOpenCodeCredentialPrecedenceAndCookiePermissions(t *testing.T) {
	dir := t.TempDir()
	auth := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(auth, []byte(`{"opencode-go":{"type":"api","key":"cli-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"OPENCODE_DATA_DIR": dir, "OPENCODE_GO_API_KEY": "explicit-key"}
	config := Environment{Getenv: func(name string) string { return env[name] }, GOOS: "linux"}
	if key, err := config.OpenCodeGoAPIKey("pi-key"); err != nil || key != "explicit-key" {
		t.Fatal(key, err)
	}
	delete(env, "OPENCODE_GO_API_KEY")
	if key, err := config.OpenCodeGoAPIKey("pi-key"); err != nil || key != "pi-key" {
		t.Fatal(key, err)
	}
	if key, err := config.OpenCodeGoAPIKey(""); err != nil || key != "cli-key" {
		t.Fatal(key, err)
	}
	cookie := filepath.Join(dir, "cookie.json")
	if err := os.WriteFile(cookie, []byte(`{"workspaceId":"workspace","authCookie":"private"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env["OPENCODE_GO_QUOTA_CONFIG"] = cookie
	if _, ok := config.OpenCodeGoCookieConfig(); ok {
		t.Fatal("accepted shared cookie file")
	}
	if err := os.Chmod(cookie, 0o600); err != nil {
		t.Fatal(err)
	}
	if value, ok := config.OpenCodeGoCookieConfig(); !ok || value.WorkspaceID != "workspace" {
		t.Fatal(value, ok)
	}
}
