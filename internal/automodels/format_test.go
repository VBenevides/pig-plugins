package automodels

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Expectations follow the pinned format.ts formulas; they are not generated
// from these Go helpers. Timestamp expectations pin upstream's en-US/UTC locale.
func TestTimeAndAgeFormatting(t *testing.T) {
	for _, test := range []struct {
		duration time.Duration
		want     string
	}{
		{-time.Second, "expired"}, {0, "expired"}, {time.Nanosecond, "0min"}, {59 * time.Second, "0min"}, {time.Minute, "1min"}, {59*time.Minute + 59*time.Second, "59min"}, {time.Hour, "1h 0min"}, {2*time.Hour + 3*time.Minute + 59*time.Second, "2h 3min"},
	} {
		if got := FormatTimeLeft(test.duration); got != test.want {
			t.Errorf("left %s: got %q, want %q", test.duration, got, test.want)
		}
	}
	for _, test := range []struct {
		duration time.Duration
		want     string
	}{
		{-500 * time.Millisecond, "0s ago"}, {0, "0s ago"}, {499 * time.Millisecond, "0s ago"}, {500 * time.Millisecond, "1s ago"}, {59499 * time.Millisecond, "59s ago"}, {59500 * time.Millisecond, "1min ago"}, {89500 * time.Millisecond, "2min ago"}, {5 * time.Hour, "300min ago"},
	} {
		if got := FormatAge(test.duration); got != test.want {
			t.Errorf("age %s: got %q, want %q", test.duration, got, test.want)
		}
	}
}
func TestDisplayHelpers(t *testing.T) {
	for _, test := range []struct {
		number float64
		want   string
	}{
		{0, "0"}, {999, "999"}, {1000, "1K"}, {1500, "2K"}, {999999, "1000K"}, {1000000, "1.0M"}, {1250000, "1.3M"}, {-1000, "-1000"},
	} {
		if got := FormatTokens(test.number); got != test.want {
			t.Errorf("tokens %v: %q != %q", test.number, got, test.want)
		}
	}
	for _, test := range []struct {
		percent float64
		want    string
	}{
		{-10, "[░░░░░░░░░░░░░░░░░░░░]"}, {0, "[░░░░░░░░░░░░░░░░░░░░]"}, {2.49, "[░░░░░░░░░░░░░░░░░░░░]"}, {2.5, "[█░░░░░░░░░░░░░░░░░░░]"}, {50, "[██████████░░░░░░░░░░]"}, {110, "[████████████████████]"},
	} {
		if got := MakeBar(test.percent); got != test.want {
			t.Errorf("bar %v: %q != %q", test.percent, got, test.want)
		}
	}
	for _, test := range []struct {
		seconds float64
		want    string
	}{
		{18000, "5h"}, {9000, "3h"}, {86400, "1d"}, {129600, "2d"}, {604800, "Weekly"}, {2592000, "30d"},
	} {
		if got := FormatWindowLabel(test.seconds); got != test.want {
			t.Errorf("window %v: %q != %q", test.seconds, got, test.want)
		}
	}
}
func TestClaudeUsageFormatting(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	limits := []ClaudeLimit{
		{Kind: "session", Percent: new(12.5), Severity: "normal", ResetsAt: "2026-01-02T03:04:00Z"},
		{Kind: "weekly_all", Percent: new(100.0), Severity: "warning"},
		{Kind: "weekly_scoped", Percent: new(50.0), Scope: &ClaudeScope{Model: &ClaudeModel{DisplayName: new("Fable")}}},
		{Kind: "weekly_scoped"},
		{},
	}
	want := []string{
		fmt.Sprintf("  📈 %-13s [███░░░░░░░░░░░░░░░░░]  13%% · resets 1/2, 03:04 AM", "5h"),
		fmt.Sprintf("  📈 %-13s [████████████████████] 100%% (warning)", "Weekly"),
		fmt.Sprintf("  📈 %-13s [██████████░░░░░░░░░░]  50%%", "Fable weekly"),
		fmt.Sprintf("  📈 %-13s [░░░░░░░░░░░░░░░░░░░░]   0%%", "Scoped weekly"),
		fmt.Sprintf("  📈 %-13s [░░░░░░░░░░░░░░░░░░░░]   0%%", "?"),
	}
	if got := FormatClaudeUsageLines(limits, now); !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(FormatClaudeUsageLines(nil, now)) != 0 {
		t.Fatal("empty usage must not produce rows")
	}
	// Empty display names are present, not null; preserve upstream nullish fallback.
	got := FormatClaudeUsageLines([]ClaudeLimit{{Kind: "weekly_scoped", Scope: &ClaudeScope{Model: &ClaudeModel{DisplayName: new("")}}}}, now)
	if !strings.HasPrefix(got[0], "  📈  weekly ") {
		t.Fatalf("empty display name: %q", got[0])
	}
	zone := time.FixedZone("UTC+2", 2*3600)
	got = FormatClaudeUsageLines(limits[:1], now.In(zone))
	if !strings.HasSuffix(got[0], "resets 1/2, 05:04 AM") {
		t.Fatalf("explicit zone ignored: %q", got[0])
	}
}
func TestCodexUsageFormattingAdditionalLimits(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	windows := []CodexWindow{{UsedPercent: 50, LimitWindowSeconds: 18000, ResetAfterSeconds: 604800, ResetAt: 0}}
	additional := []CodexAdditionalRateLimit{
		{LimitName: "GPT-5.3-Codex-Spark", RateLimit: &CodexRateLimit{PrimaryWindow: &CodexWindow{UsedPercent: 12.5, LimitWindowSeconds: 18000}, SecondaryWindow: &CodexWindow{UsedPercent: 100, LimitWindowSeconds: 604800, ResetAfterSeconds: 604800}}},
		{MeteredFeature: "feature-image", RateLimit: &CodexRateLimit{PrimaryWindow: &CodexWindow{LimitWindowSeconds: 86400}}},
		{RateLimit: &CodexRateLimit{PrimaryWindow: &CodexWindow{UsedPercent: 5, LimitWindowSeconds: 3600}}},
		{LimitName: "ignored"},
	}
	want := []string{
		"  📈 5h: no recent activity",
		fmt.Sprintf("  📈 %-12s [██████████░░░░░░░░░░]  50%% (pro) · resets 1/1, 12:00 AM", "Weekly"),
		fmt.Sprintf("  📈 %-12s [███░░░░░░░░░░░░░░░░░]  13%% · resets 1/1, 12:00 AM", "Spark 5h"),
		fmt.Sprintf("  📈 %-12s [████████████████████] 100%% · resets 1/1, 12:00 AM", "Spark weekly"),
		fmt.Sprintf("  📈 %-12s [░░░░░░░░░░░░░░░░░░░░]   0%% · resets 1/1, 12:00 AM", "image 1d"),
		fmt.Sprintf("  📈 %-12s [█░░░░░░░░░░░░░░░░░░░]   5%% · resets 1/1, 12:00 AM", "extra 1h"),
	}
	if got := FormatCodexUsageLines(windows, "pro", additional, now); !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	windows[0].ResetAfterSeconds = 86400
	if got := FormatCodexUsageLines(windows, "", nil, now); len(got) != 1 || strings.Contains(got[0], "no recent activity") || strings.Contains(got[0], "()") {
		t.Fatalf("session boundary: %v", got)
	}
	if got := FormatCodexUsageLines(nil, "pro", additional[:1], now); len(got) != 2 || strings.Contains(got[0], "no recent activity") || strings.Contains(got[0], "(pro)") {
		t.Fatalf("extras without base windows: %v", got)
	}
	if len(FormatCodexUsageLines(nil, "", nil, now)) != 0 {
		t.Fatal("empty usage must not produce rows")
	}
}
func TestPassiveQuotaFormatting(t *testing.T) {
	// No timestamps here, so this test is independent of the process timezone.
	info := RateLimitInfo{Utilization: "0.5", Status: "allowed", WeeklyUtilization: "0.99", WeeklyStatus: "warning", RequestsLimit: "100", RequestsRemaining: "0", TokensLimit: "1000000", TokensRemaining: "250000"}
	want := []string{
		"  📈 5h       [██████████░░░░░░░░░░]  50%",
		"  📈 Weekly   [████████████████████]  99% (warning)",
		"  📈 Requests [████████████████████] 100% (100/100)",
		"  📈 Tokens   [███████████████░░░░░]  75% (750K/1.0M)",
	}
	if got := FormatPassiveRateLimitLines(info); !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	info = RateLimitInfo{TokensLimit: "100", TokensRemaining: "50", TokensReset: "invalid-reset"}
	if got := FormatPassiveRateLimitLines(info); len(got) != 1 || !strings.HasSuffix(got[0], "resets invalid-reset") {
		t.Fatalf("token reset fallback: %v", got)
	}
	info.Reset = "invalid-session-reset"
	if got := FormatPassiveRateLimitLines(info); strings.Contains(got[0], "resets") {
		t.Fatalf("token reset must not override session reset: %v", got)
	}
	info.Reset, info.WeeklyReset = "", "invalid-weekly-reset"
	if got := FormatPassiveRateLimitLines(info); strings.Contains(got[0], "resets") {
		t.Fatalf("token reset must not override weekly reset: %v", got)
	}
	if len(FormatPassiveRateLimitLines(RateLimitInfo{})) != 0 {
		t.Fatal("empty passive limits must not produce rows")
	}
}
