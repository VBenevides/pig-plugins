package automodels

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestHeaderParsing(t *testing.T) {
	now := time.Unix(1700000000, 123000000)
	if ParseAnthropicHeaders(nil, now) != nil || ParseOpenAIHeaders(nil, now) != nil {
		t.Fatal("empty headers must not produce a quota")
	}
	anthropic := ParseAnthropicHeaders(map[string]string{
		"anthropic-ratelimit-unified-5h-utilization": "0",
		"anthropic-ratelimit-unified-5h-status":      "allowed",
		"anthropic-ratelimit-unified-5h-reset":       "1700000050",
		"anthropic-ratelimit-unified-7d-utilization": "0.9",
		"anthropic-ratelimit-unified-7d-status":      "warning",
		"anthropic-ratelimit-unified-7d-reset":       "1700600000",
		"anthropic-ratelimit-requests-limit":         "100",
	}, now)
	want := &RateLimitInfo{Utilization: "0", Status: "allowed", Reset: "1700000050", WeeklyUtilization: "0.9", WeeklyStatus: "warning", WeeklyReset: "1700600000", CapturedAt: float64(now.UnixMilli())}
	if !reflect.DeepEqual(anthropic, want) {
		t.Fatalf("unified headers precedence: got %+v, want %+v", anthropic, want)
	}
	for _, prefix := range []string{"anthropic-ratelimit-", "x-ratelimit-"} {
		headers := map[string]string{}
		fields := []string{"requests-limit", "requests-remaining", "requests-reset", "tokens-limit", "tokens-remaining", "tokens-reset"}
		if prefix == "x-ratelimit-" {
			fields = []string{"limit-requests", "remaining-requests", "reset-requests", "limit-tokens", "remaining-tokens", "reset-tokens"}
		}
		for i, field := range fields {
			headers[prefix+field] = []string{"100", "0", "20s", "5000", "4500", "1m"}[i]
		}
		var got *RateLimitInfo
		if prefix == "x-ratelimit-" {
			got = ParseOpenAIHeaders(headers, now)
		} else {
			got = ParseAnthropicHeaders(headers, now)
		}
		want := &RateLimitInfo{RequestsLimit: "100", RequestsRemaining: "0", RequestsReset: "20s", TokensLimit: "5000", TokensRemaining: "4500", TokensReset: "1m", CapturedAt: float64(now.UnixMilli())}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %+v", prefix, got)
		}
	}
}

func TestPassiveCooldownBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, test := range []struct {
		name string
		info *RateLimitInfo
		want time.Duration
	}{
		{"absent", nil, 0},
		{"below threshold", &RateLimitInfo{Utilization: "0.989", Reset: "1700000060"}, 0},
		{"threshold", &RateLimitInfo{Utilization: "0.99", Reset: "1700000060"}, time.Minute},
		{"exact stale boundary", &RateLimitInfo{Utilization: "1", Reset: "1700000060", CapturedAt: float64(now.Add(-5 * time.Hour).UnixMilli())}, time.Minute},
		{"stale", &RateLimitInfo{Utilization: "1", Reset: "1700000060", CapturedAt: float64(now.Add(-5*time.Hour - time.Millisecond).UnixMilli())}, 0},
		{"expired", &RateLimitInfo{Utilization: "1", Reset: "1700000000"}, 0},
		{"invalid reset", &RateLimitInfo{Utilization: "1", Reset: "NaN"}, 0},
		{"infinite reset", &RateLimitInfo{Utilization: "1", Reset: "Infinity"}, 0},
		{"weekly only", &RateLimitInfo{WeeklyUtilization: "1", WeeklyReset: "1700000060"}, 0},
		// Upstream Number(NaN) < .99 is false; keep that conservative behavior.
		{"invalid utilization", &RateLimitInfo{Utilization: "invalid", Reset: "1700000060"}, time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := PassiveCooldown(test.info, now); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
	if IsStale(nil, now) || IsStale(&RateLimitInfo{}, now) {
		t.Fatal("uncaptured information is not stale")
	}
}
func TestCooldownPrecedence(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, test := range []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"default", nil, 5 * time.Hour},
		{"unified reset first", map[string]string{"anthropic-ratelimit-unified-5h-reset": "1700000060", "anthropic-ratelimit-unified-reset": "1700000120", "retry-after-ms": "10", "retry-after": "20"}, time.Minute},
		{"legacy unified reset", map[string]string{"anthropic-ratelimit-unified-reset": "1700000120"}, 2 * time.Minute},
		{"empty new reset does not select legacy", map[string]string{"anthropic-ratelimit-unified-5h-reset": "", "anthropic-ratelimit-unified-reset": "1700000120", "retry-after-ms": "10"}, 10 * time.Millisecond},
		{"past reset then milliseconds", map[string]string{"anthropic-ratelimit-unified-5h-reset": "1699999999", "retry-after-ms": "1500", "retry-after": "20"}, 1500 * time.Millisecond},
		{"invalid milliseconds then seconds", map[string]string{"retry-after-ms": "bad", "retry-after": "1.5"}, 1500 * time.Millisecond},
		{"date retry unsupported upstream", map[string]string{"retry-after": "Wed, 21 Oct 2015 07:28:00 GMT"}, 5 * time.Hour},
		{"nonpositive", map[string]string{"retry-after-ms": "0", "retry-after": "-1"}, 5 * time.Hour},
		{"large bounded", map[string]string{"retry-after-ms": "Infinity"}, time.Duration(math.MaxInt64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Cooldown(test.headers, now); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}
func TestQuotaSelection(t *testing.T) {
	if ClaudeAvailable(nil) != nil || ClaudeAvailable(&ClaudeUsage{}) != nil || ClaudeStatusQuota(nil) != nil || CodexStatusQuota(nil) != nil {
		t.Fatal("missing usage must remain unknown")
	}
	usage := &ClaudeUsage{Limits: []ClaudeLimit{{Kind: "weekly_scoped", Percent: new(100.0)}, {Kind: "weekly_all", Percent: new(70.0)}, {Kind: "session", Percent: new(12.5)}}}
	if got := ClaudeAvailable(usage); got == nil || *got {
		t.Fatal("scoped exhaustion must make Claude unavailable")
	}
	if got := ClaudeStatusQuota(usage); !reflect.DeepEqual(got, &StatusQuota{"5h", 13}) {
		t.Fatalf("got %+v", got)
	}
	usage.Limits = []ClaudeLimit{{Kind: "weekly_all", Percent: new(45.5)}}
	if got := ClaudeStatusQuota(usage); !reflect.DeepEqual(got, &StatusQuota{"Weekly", 46}) {
		t.Fatalf("got %+v", got)
	}
	usage.Limits = []ClaudeLimit{{Kind: "session"}, {Kind: "weekly_all", Percent: new(70.0)}}
	if ClaudeStatusQuota(usage) != nil {
		t.Fatal("a present session without percent must not fall back")
	}
	if available := ClaudeAvailable(usage); available == nil || !*available {
		t.Fatal("absent percent defaults to zero")
	}
	primary := &CodexWindow{UsedPercent: 50.5, LimitWindowSeconds: 18000, ResetAfterSeconds: 86401}
	secondary := &CodexWindow{UsedPercent: 12.5, LimitWindowSeconds: 18000, ResetAfterSeconds: 86400}
	codex := &CodexUsage{RateLimit: &CodexRateLimit{PrimaryWindow: primary, SecondaryWindow: secondary}}
	if got := CodexStatusQuota(codex); !reflect.DeepEqual(got, &StatusQuota{"5h", 13}) {
		t.Fatalf("live session precedence: %+v", got)
	}
	secondary.ResetAfterSeconds = 604800
	if got := CodexStatusQuota(codex); !reflect.DeepEqual(got, &StatusQuota{"Weekly", 13}) {
		t.Fatalf("latest reset weekly fallback: %+v", got)
	}
}
func TestRateLimitErrorAndModelFlags(t *testing.T) {
	for _, message := range []string{"HTTP 429", "rate_limit_error", "RATE LIMIT reached"} {
		if !RateLimitError(message) {
			t.Errorf("missed %q", message)
		}
	}
	for _, message := range []string{"", "1429", "4290", "unrelated error"} {
		if RateLimitError(message) {
			t.Errorf("false positive %q", message)
		}
	}
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"--model", "x"}, true}, {[]string{"--model=x"}, true}, {[]string{"--", "--model=x"}, false}, {[]string{"--model=x", "--"}, true}, {[]string{"--models", "x"}, false},
	} {
		if got := HasExplicitModelFlag(test.args); got != test.want {
			t.Errorf("%v: %v", test.args, got)
		}
	}
}
