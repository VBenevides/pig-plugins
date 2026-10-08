package automodels

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultCooldown = 5 * time.Hour

func ParseAnthropicHeaders(headers map[string]string, now time.Time) *RateLimitInfo {
	if utilization := headers["anthropic-ratelimit-unified-5h-utilization"]; utilization != "" {
		return &RateLimitInfo{
			Utilization:       utilization,
			Status:            headers["anthropic-ratelimit-unified-5h-status"],
			Reset:             headers["anthropic-ratelimit-unified-5h-reset"],
			WeeklyUtilization: headers["anthropic-ratelimit-unified-7d-utilization"],
			WeeklyStatus:      headers["anthropic-ratelimit-unified-7d-status"],
			WeeklyReset:       headers["anthropic-ratelimit-unified-7d-reset"],
			CapturedAt:        float64(now.UnixMilli()),
		}
	}
	return parseRequestHeaders(headers, "anthropic-ratelimit-", "requests-limit", "requests-remaining", "requests-reset", "tokens-limit", "tokens-remaining", "tokens-reset", now)
}
func ParseOpenAIHeaders(headers map[string]string, now time.Time) *RateLimitInfo {
	return parseRequestHeaders(headers, "x-ratelimit-", "limit-requests", "remaining-requests", "reset-requests", "limit-tokens", "remaining-tokens", "reset-tokens", now)
}
func parseRequestHeaders(headers map[string]string, prefix, requestsLimit, requestsRemaining, requestsReset, tokensLimit, tokensRemaining, tokensReset string, now time.Time) *RateLimitInfo {
	limit := headers[prefix+requestsLimit]
	if limit == "" {
		return nil
	}
	return &RateLimitInfo{
		RequestsLimit: limit, RequestsRemaining: headers[prefix+requestsRemaining], RequestsReset: headers[prefix+requestsReset],
		TokensLimit: headers[prefix+tokensLimit], TokensRemaining: headers[prefix+tokensRemaining], TokensReset: headers[prefix+tokensReset],
		CapturedAt: float64(now.UnixMilli()),
	}
}

func IsStale(info *RateLimitInfo, now time.Time) bool {
	return info != nil && info.CapturedAt != 0 && float64(now.UnixMilli())-info.CapturedAt > float64(defaultCooldown/time.Millisecond)
}
func PassiveCooldown(info *RateLimitInfo, now time.Time) time.Duration {
	if info == nil || info.Utilization == "" || IsStale(info, now) || jsNumber(info.Utilization) < 0.99 || info.Reset == "" {
		return 0
	}
	reset := jsNumber(info.Reset)
	if !finite(reset) {
		return 0
	}
	return positiveDuration((reset*1000 - float64(now.UnixMilli())) * float64(time.Millisecond))
}
func Cooldown(headers map[string]string, now time.Time) time.Duration {
	reset, exists := headers["anthropic-ratelimit-unified-5h-reset"]
	if !exists {
		reset = headers["anthropic-ratelimit-unified-reset"]
	}
	if reset != "" {
		seconds := jsNumber(reset)
		if finite(seconds) {
			if duration := positiveDuration((seconds*1000 - float64(now.UnixMilli())) * float64(time.Millisecond)); duration > 0 {
				return duration
			}
		}
	}
	if ms := jsNumber(headers["retry-after-ms"]); ms > 0 {
		return positiveDuration(ms * float64(time.Millisecond))
	}
	if seconds := jsNumber(headers["retry-after"]); seconds > 0 {
		return positiveDuration(seconds * float64(time.Second))
	}
	return defaultCooldown
}

var rateLimitPattern = regexp.MustCompile(`(?i)\b429\b|rate_limit_error|rate limit`)

func RateLimitError(message string) bool { return rateLimitPattern.MatchString(message) }

func ClaudeAvailable(usage *ClaudeUsage) *bool {
	if usage == nil || len(usage.Limits) == 0 {
		return nil
	}
	known := false
	for _, limit := range usage.Limits {
		if limit.Percent == nil || !finite(*limit.Percent) {
			continue
		}
		known = true
		if *limit.Percent >= 100 {
			available := false
			return &available
		}
	}
	if !known {
		return nil
	}
	available := true
	return &available
}

// CodexAvailable considers both the short and weekly windows. A display badge
// for one window alone is not evidence that the account can serve a request.
func CodexAvailable(usage *CodexUsage) *bool {
	if usage == nil || usage.RateLimit == nil {
		return nil
	}
	limit := usage.RateLimit
	available := false
	if limit.LimitReached {
		return &available
	}
	known := false
	for _, window := range []*CodexWindow{limit.PrimaryWindow, limit.SecondaryWindow} {
		if window == nil {
			continue
		}
		if !finite(window.UsedPercent) || window.UsedPercent < 0 {
			return nil
		}
		known = true
		if window.UsedPercent >= 100 {
			return &available
		}
	}
	if !known {
		return nil
	}
	available = limit.Allowed
	return &available
}

type StatusQuota struct {
	Label   string
	Percent int
}

func ClaudeStatusQuota(usage *ClaudeUsage) *StatusQuota {
	if usage == nil {
		return nil
	}
	for _, kind := range []string{"session", "weekly_all"} {
		for _, limit := range usage.Limits {
			if limit.Kind == kind {
				if limit.Percent == nil {
					return nil
				}
				label := "Weekly"
				if kind == "session" {
					label = "5h"
				}
				return &StatusQuota{Label: label, Percent: roundedInt(*limit.Percent)}
			}
		}
	}
	return nil
}
func CodexStatusQuota(usage *CodexUsage) *StatusQuota {
	if usage == nil || usage.RateLimit == nil {
		return nil
	}
	var latest *CodexWindow
	for _, window := range []*CodexWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
		if window == nil {
			continue
		}
		if window.LimitWindowSeconds <= 24*3600 && window.ResetAfterSeconds <= 24*3600 {
			return &StatusQuota{Label: "5h", Percent: roundedInt(window.UsedPercent)}
		}
		if latest == nil || window.ResetAfterSeconds > latest.ResetAfterSeconds {
			latest = window
		}
	}
	if latest == nil {
		return nil
	}
	return &StatusQuota{Label: "Weekly", Percent: roundedInt(latest.UsedPercent)}
}

// JavaScript Number conversion is shared by the original header and display helpers.
func jsNumber(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if value == "Infinity" || value == "+Infinity" {
		return math.Inf(1)
	}
	if value == "-Infinity" {
		return math.Inf(-1)
	}
	if len(value) > 2 && value[0] == '0' {
		base := 0
		switch value[1] {
		case 'x', 'X':
			base = 16
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		}
		if base != 0 {
			n, err := strconv.ParseUint(value[2:], base, 64)
			if err == nil {
				return float64(n)
			}
			return math.NaN()
		}
	}
	// Go accepts Inf and hexadecimal floats, which Number does not.
	if strings.ContainsAny(value, "xXpP_") || strings.Contains(value, "Inf") {
		return math.NaN()
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return math.NaN()
	}
	return n
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func roundedInt(value float64) int {
	if !finite(value) {
		return 0
	}
	value = math.Floor(value + 0.5)
	if value >= float64(math.MaxInt) {
		return math.MaxInt
	}
	if value <= float64(math.MinInt) {
		return math.MinInt
	}
	return int(value)
}
