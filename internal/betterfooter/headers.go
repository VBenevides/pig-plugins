package betterfooter

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RawWindow is one rate-limit group found in response headers, before conversion to a display window.
type RawWindow struct {
	Key, Scope string
	// Limit, Remaining and Used are nil when the header was absent or not a finite number.
	Limit, Remaining, Used *float64
	// Reset is the raw reset value: seconds, a duration like "6m0s", or an ISO date.
	Reset *string
}

var headerRules = []struct {
	re    *regexp.Regexp
	field string
}{
	{regexp.MustCompile(`^(.+)-(.+)-reset-ttl$`), "reset"},
	{regexp.MustCompile(`^(.+)-reset-ttl-(.+)$`), "reset"},
	{regexp.MustCompile(`^(.+)-(.+)-reset$`), "reset"},
	{regexp.MustCompile(`^(.+)-reset-(.+)$`), "reset"},
	{regexp.MustCompile(`^(.+)-(.+)-remaining$`), "remaining"},
	{regexp.MustCompile(`^(.+)-remaining-(.+)$`), "remaining"},
	{regexp.MustCompile(`^(.+)-(.+)-used$`), "used"},
	{regexp.MustCompile(`^(.+)-used-(.+)$`), "used"},
	{regexp.MustCompile(`^(.+)-(.+)-limit$`), "limit"},
	{regexp.MustCompile(`^(.+)-limit-(.+)$`), "limit"},
}

func parseNumber(v string) *float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}

var durationPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(ms|s|m|h|d)`)

// parseDuration sums "6m0s", "1h30m", "500ms", "2d". No \b after the unit: OpenAI concatenates durations.
func parseDuration(text string) (float64, bool) {
	total, matched := 0.0, false
	for _, m := range durationPattern.FindAllStringSubmatch(text, -1) {
		matched = true
		v, _ := strconv.ParseFloat(m[1], 64)
		switch strings.ToLower(m[2]) {
		case "ms":
			total += v / 1000
		case "s":
			total += v
		case "m":
			total += v * 60
		case "h":
			total += v * 3600
		case "d":
			total += v * 86400
		}
	}
	return total, matched
}

var (
	plainNumber = regexp.MustCompile(`^\d+(\.\d+)?$`)
	isoHint     = regexp.MustCompile(`[T:-]`)
	isoDate     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

// parseDate is Date.parse for the ISO forms providers send: zoned timestamps, zone-less local date-times, dates.
func parseDate(text string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"} {
		location := time.Local
		if layout == "2006-01-02" {
			location = time.UTC
		}
		if t, err := time.ParseInLocation(layout, text, location); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ParseReset converts a reset header value to seconds from now. A plain number is seconds, unless it is clearly
// an absolute epoch in seconds or milliseconds.
func ParseReset(raw string, now time.Time) (float64, bool) {
	v := strings.TrimSpace(raw)
	if plainNumber.MatchString(v) {
		n, _ := strconv.ParseFloat(v, 64)
		nowSec := float64(now.UnixMilli()) / 1000
		switch {
		case n >= 1e12:
			return math.Max(0, (n-float64(now.UnixMilli()))/1000), true
		case n >= 1e9:
			return math.Max(0, n-nowSec), true
		}
		return n, true
	}
	if isoHint.MatchString(v) && isoDate.MatchString(v) {
		if t, ok := parseDate(v); ok {
			return math.Max(0, t.Sub(now).Seconds()), true
		}
	}
	return parseDuration(v)
}

// DetectRateWindows groups limit/remaining/used/reset headers by their prefix and scope, understanding the
// OpenAI and Anthropic naming. Header names are matched lower-cased; the order of the result follows the sorted
// header names so it is deterministic.
func DetectRateWindows(headers map[string]string) []RawWindow {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	slices.Sort(names)
	var order []string
	groups := map[string]*RawWindow{}
	for _, original := range names {
		value := strings.TrimSpace(headers[original])
		if value == "" {
			continue
		}
		key := strings.ToLower(original)
		for _, rule := range headerRules {
			m := rule.re.FindStringSubmatch(key)
			if m == nil {
				continue
			}
			id := m[1] + "|" + m[2]
			w := groups[id]
			if w == nil {
				w = &RawWindow{Key: id, Scope: m[2]}
				groups[id] = w
				order = append(order, id)
			}
			switch rule.field {
			case "limit":
				w.Limit = parseNumber(value)
			case "remaining":
				w.Remaining = parseNumber(value)
			case "used":
				w.Used = parseNumber(value)
			default:
				v := value
				w.Reset = &v
			}
			break
		}
	}
	out := make([]RawWindow, 0, len(order))
	for _, id := range order {
		out = append(out, *groups[id])
	}
	return out
}

var tokenScope = regexp.MustCompile(`(?i)token`)

// ToRateWindows converts raw windows to display windows, preferring token-scoped ones, sorted 5h before weekly.
func ToRateWindows(raw []RawWindow, now time.Time) []RateWindow {
	var withLimit []RawWindow
	for _, w := range raw {
		if w.Limit != nil && (w.Remaining != nil || w.Used != nil) {
			withLimit = append(withLimit, w)
		}
	}
	pool := slices.DeleteFunc(slices.Clone(withLimit), func(w RawWindow) bool { return !tokenScope.MatchString(w.Scope) })
	if len(pool) == 0 {
		pool = withLimit
	}
	out := make([]RateWindow, 0, len(pool))
	for _, w := range pool {
		limit := *w.Limit
		var remaining float64
		switch {
		case w.Remaining != nil:
			remaining = *w.Remaining
		case w.Used != nil:
			remaining = limit - *w.Used
		}
		percent := 0.0
		if limit > 0 {
			percent = math.Max(0, math.Min(100, remaining/limit*100))
		}
		window := RateWindow{Scope: w.Scope, Percent: percent, CapturedAt: now}
		if w.Reset != nil {
			// A reset of "0s" (or a past timestamp) means the window has just reset: keep it timed and expired.
			window.ResetSec, window.HasReset = ParseReset(*w.Reset, now)
		}
		out = append(out, window)
	}
	SortRateWindows(out)
	return out
}

var limitErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Usage limit reached for\s+(.+?)\.\s*Your limit will reset at\s+(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2})`),
	regexp.MustCompile(`已达到\s*(.+?)的?使用上限[\s\S]*?将在\s*(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2})\s*重置`),
}

// zaiResetZone is Beijing time: both Z.AI gateways print the reset zone-less, whatever the client's timezone.
var zaiResetZone = time.FixedZone("UTC+8", 8*3600)

var (
	hoursScope = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:hour|小时)`)
	weekScope  = regexp.MustCompile(`(?i)week|周`)
)

// ParseLimitError reads the exhausted window out of a Z.AI 429 body (the quota-monitor endpoint fallback). A 429
// means the window is fully used, so it has 0% left. It returns false when no reset in the future is found.
func ParseLimitError(message string, now time.Time) (RateWindow, bool) {
	var m []string
	for _, pattern := range limitErrorPatterns {
		if m = pattern.FindStringSubmatch(message); m != nil {
			break
		}
	}
	if m == nil {
		return RateWindow{}, false
	}
	resetAt, err := time.ParseInLocation("2006-01-02 15:04:05", m[2]+" "+m[3], zaiResetZone)
	if err != nil {
		return RateWindow{}, false
	}
	resetSec := resetAt.Sub(now).Seconds()
	if resetSec <= 0 {
		return RateWindow{}, false
	}
	scope := strings.ToLower(strings.TrimSpace(m[1]))
	window := RateWindow{Percent: 0, HasReset: true, ResetSec: resetSec, CapturedAt: now}
	if h := hoursScope.FindStringSubmatch(scope); h != nil {
		hours, _ := strconv.ParseFloat(h[1], 64)
		window.WindowMins = hours * 60
		if hours == 5 {
			scope = "zai:3"
		}
	} else if weekScope.MatchString(scope) {
		window.WindowMins = 7 * 24 * 60
		scope = "zai:6"
	}
	window.Scope = scope
	return window, true
}
