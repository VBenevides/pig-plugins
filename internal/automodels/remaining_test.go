package automodels

import (
	"encoding/json"
	"testing"
)

func TestRemainingQuotaWindows(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       *float64
	}{
		{"weekly lower", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":10},"secondary_window":{"used_percent":96}}}`, number(4)},
		{"short lower", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":96},"secondary_window":{"used_percent":10}}}`, number(4)},
		{"exactly five", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":95}}}`, number(5)},
		{"fractional", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":95.1}}}`, number(4.9)},
		{"exhausted", `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":100}}}`, number(0)},
		{"denied", `{"rate_limit":{"allowed":false,"primary_window":{"used_percent":10}}}`, number(0)},
		{"reached", `{"rate_limit":{"limit_reached":true}}`, number(0)},
		{"unknown", `{}`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var usage CodexUsage
			if err := json.Unmarshal([]byte(test.body), &usage); err != nil {
				t.Fatal(err)
			}
			got := CodexRemaining(&usage)
			if (got == nil) != (test.want == nil) || got != nil && abs(*got-*test.want) > 0.0001 {
				t.Fatalf("remaining = %v, want %v", got, test.want)
			}
		})
	}
	usage := &ClaudeUsage{Limits: []ClaudeLimit{{Kind: "session", Percent: number(20)}, {Kind: "weekly_all", Percent: number(97)}}}
	if got := ClaudeRemaining(usage); got == nil || *got != 3 {
		t.Fatalf("Claude weekly remaining = %v", got)
	}
	usage.Limits[0].Percent = number(98)
	if got := ClaudeRemaining(usage); got == nil || *got != 2 {
		t.Fatalf("Claude session remaining = %v", got)
	}
	if ClaudeRemaining(nil) != nil {
		t.Fatal("unknown Claude quota reported available")
	}
}

func number(value float64) *float64 { return &value }
func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func TestQuotaFailureRecognition(t *testing.T) {
	for _, message := range []string{"insufficient_quota", "usage_limit_reached", "Quota exhausted", "You have exceeded your quota", "429 rate limit"} {
		if !RateLimitError(message) {
			t.Errorf("unrecognized quota error %q", message)
		}
	}
	for _, message := range []string{"invalid API key", "network timeout", "quota status unavailable"} {
		if RateLimitError(message) {
			t.Errorf("non-quota error matched %q", message)
		}
	}
}
