package automodels

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

func FormatTimeLeft(left time.Duration) string {
	if left <= 0 {
		return "expired"
	}
	hours := left / time.Hour
	minutes := left % time.Hour / time.Minute
	if hours > 0 {
		return fmt.Sprintf("%dh %dmin", hours, minutes)
	}
	return fmt.Sprintf("%dmin", minutes)
}

// FormatAge takes elapsed time, rather than upstream's capture timestamp.
func FormatAge(age time.Duration) string {
	seconds := math.Floor(float64(age)/float64(time.Second) + 0.5)
	if seconds < 60 {
		return numberText(seconds) + "s ago"
	}
	return numberText(math.Floor(seconds/60+0.5)) + "min ago"
}
func FormatTokens(n float64) string {
	if n >= 1_000_000 {
		return strconv.FormatFloat(math.Floor(n/100_000+0.5)/10, 'f', 1, 64) + "M"
	}
	if n >= 1_000 {
		return numberText(math.Floor(n/1000+0.5)) + "K"
	}
	return numberText(n)
}
func MakeBar(percent float64) string {
	percent = math.Max(0, math.Min(100, percent))
	filled := 0
	if !math.IsNaN(percent) {
		filled = roundedInt(percent / 5)
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", 20-filled) + "]"
}
func FormatWindowLabel(seconds float64) string {
	hours := seconds / 3600
	if hours >= 24 {
		days := math.Floor(hours/24 + 0.5)
		if days == 7 {
			return "Weekly"
		}
		return numberText(days) + "d"
	}
	return numberText(math.Floor(hours+0.5)) + "h"
}

// Reset timestamps use deterministic en-US-style text instead of the upstream
// process-dependent locale. Explicit now arguments control the display zone.
func resetTime(reset string, location *time.Location) (time.Time, bool) {
	seconds := jsNumber(reset)
	if finite(seconds) && math.Abs(seconds) <= 8.64e12 {
		whole, fraction := math.Modf(seconds)
		return time.Unix(int64(whole), int64(fraction*1e9)).In(location), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC850, time.UnixDate, time.RubyDate, "2006-01-02"} {
		if parsed, err := time.Parse(layout, reset); err == nil {
			return parsed.In(location), true
		}
	}
	return time.Time{}, false
}
func FormatReset(reset string) string {
	if parsed, ok := resetTime(reset, time.Local); ok {
		return parsed.Format("1/2/2006, 3:04:05 PM")
	}
	return reset
}
func shortReset(reset string, location *time.Location) string {
	if parsed, ok := resetTime(reset, location); ok {
		return parsed.Format("1/2, 03:04 PM")
	}
	return reset
}

type quotaRow struct {
	label    string
	percent  float64
	note     string
	reset    string
	hasReset bool
}

func renderQuotaRows(rows []quotaRow, location *time.Location) []string {
	width := 0
	for _, row := range rows {
		if length := jsLength(row.label); length > width {
			width = length
		}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		percent := numberText(row.percent)
		padding := ""
		if len(percent) < 3 {
			padding = strings.Repeat(" ", 3-len(percent))
		}
		line := "  📈 " + row.label + strings.Repeat(" ", width-jsLength(row.label)) + " " + MakeBar(row.percent) + " " + padding + percent + "%"
		if row.note != "" {
			line += " (" + row.note + ")"
		}
		if row.hasReset {
			line += " · resets " + shortReset(row.reset, location)
		}
		lines = append(lines, line)
	}
	return lines
}
func jsLength(value string) int {
	length := 0
	for _, r := range value {
		length += utf16.RuneLen(r)
	}
	return length
}
func numberText(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "Infinity"
	}
	if math.IsInf(value, -1) {
		return "-Infinity"
	}
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
func roundedPercent(value float64) float64 { return math.Floor(value + 0.5) }

func FormatClaudeUsageLines(limits []ClaudeLimit, now time.Time) []string {
	rows := make([]quotaRow, 0, len(limits))
	for _, limit := range limits {
		label := limit.Kind
		switch limit.Kind {
		case "session":
			label = "5h"
		case "weekly_all":
			label = "Weekly"
		case "weekly_scoped":
			name := "Scoped"
			if limit.Scope != nil && limit.Scope.Model != nil && limit.Scope.Model.DisplayName != nil {
				name = *limit.Scope.Model.DisplayName
			}
			label = name + " weekly"
		case "":
			label = "?"
		}
		percent := 0.0
		if limit.Percent != nil {
			percent = *limit.Percent
		}
		note := limit.Severity
		if note == "normal" {
			note = ""
		}
		rows = append(rows, quotaRow{label: label, percent: roundedPercent(percent), note: note, reset: limit.ResetsAt, hasReset: limit.ResetsAt != ""})
	}
	return renderQuotaRows(rows, now.Location())
}
func codexWindowLabel(window CodexWindow) string {
	if window.ResetAfterSeconds > 24*3600 {
		return "Weekly"
	}
	return FormatWindowLabel(window.LimitWindowSeconds)
}
func FormatCodexUsageLines(windows []CodexWindow, plan string, additional []CodexAdditionalRateLimit, now time.Time) []string {
	rows := make([]quotaRow, 0, len(windows)+len(additional)*2)
	hasSession := false
	for i, window := range windows {
		label := codexWindowLabel(window)
		if label == "5h" {
			hasSession = true
		}
		note := ""
		if i == 0 {
			note = plan
		}
		rows = append(rows, quotaRow{label: label, percent: roundedPercent(window.UsedPercent), note: note, reset: numberText(window.ResetAt), hasReset: true})
	}
	for _, extra := range additional {
		name := extra.LimitName
		if name == "" {
			name = extra.MeteredFeature
		}
		if name == "" {
			name = "extra"
		}
		if index := strings.LastIndexByte(name, '-'); index >= 0 {
			name = name[index+1:]
		}
		if extra.RateLimit == nil {
			continue
		}
		for _, window := range []*CodexWindow{extra.RateLimit.PrimaryWindow, extra.RateLimit.SecondaryWindow} {
			if window == nil {
				continue
			}
			rows = append(rows, quotaRow{label: name + " " + strings.ToLower(codexWindowLabel(*window)), percent: roundedPercent(window.UsedPercent), reset: numberText(window.ResetAt), hasReset: true})
		}
	}
	lines := renderQuotaRows(rows, now.Location())
	if len(windows) > 0 && !hasSession {
		lines = append([]string{"  📈 5h: no recent activity"}, lines...)
	}
	return lines
}
func FormatPassiveRateLimitLines(info RateLimitInfo) []string {
	rows := make([]quotaRow, 0, 4)
	if info.Utilization != "" {
		note := info.Status
		if note == "allowed" {
			note = ""
		}
		rows = append(rows, quotaRow{label: "5h", percent: roundedPercent(jsNumber(info.Utilization) * 100), note: note, reset: info.Reset, hasReset: info.Reset != ""})
	}
	if info.WeeklyUtilization != "" {
		note := info.WeeklyStatus
		if note == "allowed" {
			note = ""
		}
		rows = append(rows, quotaRow{label: "Weekly", percent: roundedPercent(jsNumber(info.WeeklyUtilization) * 100), note: note, reset: info.WeeklyReset, hasReset: info.WeeklyReset != ""})
	}
	if info.RequestsLimit != "" && info.RequestsRemaining != "" {
		limit := jsNumber(info.RequestsLimit)
		used := limit - jsNumber(info.RequestsRemaining)
		percent := 0.0
		if limit > 0 {
			percent = roundedPercent(used / limit * 100)
		}
		rows = append(rows, quotaRow{label: "Requests", percent: percent, note: numberText(used) + "/" + numberText(limit)})
	}
	if info.TokensLimit != "" && info.TokensRemaining != "" {
		limit := jsNumber(info.TokensLimit)
		used := limit - jsNumber(info.TokensRemaining)
		percent := 0.0
		if limit > 0 {
			percent = roundedPercent(used / limit * 100)
		}
		rows = append(rows, quotaRow{label: "Tokens", percent: percent, note: FormatTokens(used) + "/" + FormatTokens(limit), reset: info.TokensReset, hasReset: info.Reset == "" && info.WeeklyReset == "" && info.TokensReset != ""})
	}
	return renderQuotaRows(rows, time.Local)
}
