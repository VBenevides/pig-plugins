package betterfooter

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ThirdRowStatusKeys are the status badges shown on a third footer row, in display order.
var ThirdRowStatusKeys = []string{"smart-approve-lancet", "pi-curator"}

// RenderState is a snapshot; the renderer does not perform IO or mutate it.
type RenderState struct {
	Cwd, Home, Branch, Version          string
	Git                                 GitChanges
	Provider, Model, Thinking, QuotaKey string
	Reasoning                           bool
	Quota                               ProviderQuota
	Totals                              Totals
	LatestHit                           float64
	HasHit                              bool
	ContextTokens                       *int
	ContextPercent                      float64
	ContextWindow                       int
	Speed                               float64
	Estimated                           bool
	Statuses                            map[string]string
}

// Theme supplies the active host palette without coupling the renderer to the SDK.
type Theme struct {
	Fg                          func(color, text string) string
	Name, Appearance, ColorFGBG string
}

func (t Theme) fg(color, text string) string {
	if t.Fg != nil {
		return t.Fg(color, text)
	}
	return text
}
func (t Theme) light() bool {
	if t.Appearance != "" {
		return t.Appearance == "light"
	}
	name := strings.ToLower(t.Name)
	if strings.Contains(name, "light") {
		return true
	}
	if strings.Contains(name, "dark") {
		return false
	}
	parts := strings.Split(t.ColorFGBG, ";")
	n, err := strconv.Atoi(parts[len(parts)-1])
	return err == nil && n >= 7
}
func (t Theme) warning() string {
	if t.light() {
		return "syntaxFunction"
	}
	return "warning"
}
func (t Theme) reset(text string) string {
	if t.light() {
		return t.fg("accent", text)
	}
	return t.fg("dim", text)
}
func (t Theme) percent(p float64, text string) string {
	color := "muted"
	if p <= 10 {
		color = "error"
	} else if p <= 30 {
		color = t.warning()
	}
	return t.fg(color, text)
}

func joinLR(width int, left, right string, gap int) string {
	lw, rw := VisibleWidth(left), VisibleWidth(right)
	if lw+gap+rw <= width {
		return left + strings.Repeat(" ", max(gap, width-lw-rw)) + right
	}
	if avail := width - lw - gap; avail > 4 {
		return left + strings.Repeat(" ", gap) + Truncate(right, avail)
	}
	if lw >= width {
		return Truncate(left, width)
	}
	return Truncate(left+" "+right, width)
}
func fitSessionLine(width int, variants []string, speed string, models []string, sep string) string {
	essential := min(2, len(models))
	for _, session := range variants {
		all := strings.Join(models, sep)
		if speed != "" && VisibleWidth(session+sep+speed)+2+VisibleWidth(all) <= width {
			return joinLR(width, session+sep+speed, all, 2)
		}
		for n := len(models); n >= essential; n-- {
			right := strings.Join(models[:n], sep)
			if VisibleWidth(session)+2+VisibleWidth(right) <= width {
				return joinLR(width, session, right, 2)
			}
		}
	}
	left := variants[len(variants)-1]
	right := Truncate(strings.Join(models[:essential], sep), max(0, width*45/100))
	avail := max(0, width-VisibleWidth(right)-2)
	if avail == 0 {
		return Truncate(left, width)
	}
	return joinLR(width, Truncate(left, avail), right, 2)
}
func compact(parts ...string) string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}
func segments(sep string, parts ...string) string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// RenderFooter follows the upstream two-row layout and its narrow-width priorities.
func RenderFooter(s RenderState, width int, t Theme, now time.Time) []string {
	dim := func(v string) string { return t.fg("dim", v) }
	muted := func(v string) string { return t.fg("muted", v) }
	sep := " " + dim("·") + " "
	provider, model := s.Provider, s.Model
	if provider == "" {
		provider = "no-provider"
	}
	if model == "" {
		model = "no-model"
	}
	pm := t.fg("accent", SanitizePlain(provider)) + "/" + dim(SanitizePlain(model))
	if s.Reasoning {
		effort := s.Thinking
		if effort == "" {
			effort = "off"
		}
		pm += " " + t.fg("accent", SanitizePlain(effort))
	}
	models := []string{pm}
	if QuotaSource(s.QuotaKey) == ChatGPTQuotaKey {
		label, color := "ChatGPT", "dim"
		if HasRecentChatGPTLimit(s.Quota.ChatGPTLimitAt, now) {
			label, color = "ChatGPT limit", "error"
		}
		models = append(models, "\x1b]8;;"+ChatGPTUsageURL+"\x1b\\"+t.fg(color, label)+"\x1b]8;;\x1b\\")
	}
	if s.Provider == CopilotProvider && s.Quota.CopilotCredits != "" {
		credits := s.Quota.CopilotCredits
		parts := strings.Split(credits, "/")
		if len(parts) == 2 {
			left, e1 := strconv.ParseFloat(parts[0], 64)
			total, e2 := strconv.ParseFloat(parts[1], 64)
			if e1 == nil && e2 == nil && total > 0 {
				credits = t.percent(left/total*100, parts[0]) + "/" + dim(parts[1])
			}
		}
		models = append(models, credits)
	}
	shown := 0
	for _, w := range s.Quota.Windows {
		if !w.Active(now) {
			continue
		}
		if shown == 3 {
			break
		}
		shown++
		pct := t.percent(w.Percent, strconv.FormatFloat(round(w.Percent), 'f', 0, 64)+"%")
		label := ""
		if w.HasReset {
			label = t.reset(FormatReset(max(0, w.Remaining(now))))
		} else if w.Scope == "zai:monthly" {
			label = t.reset("\ueeff")
		}
		models = append(models, compact(label, pct))
	}
	speed := ""
	if s.Speed > 0 && !math.IsInf(s.Speed, 0) && !math.IsNaN(s.Speed) {
		v := round(s.Speed)
		color := "muted"
		if v >= 150 {
			color = "error"
		} else if v >= 50 {
			color = t.warning()
		}
		label := strconv.FormatFloat(v, 'f', 0, 64)
		if s.Estimated {
			label = "~" + label
		}
		speed = t.fg(color, label) + dim("t/s")
	}
	keys := make([]string, 0, len(s.Statuses))
	for k := range s.Statuses {
		if !slices.Contains(ThirdRowStatusKeys, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	texts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := Sanitize(s.Statuses[k]); v != "" {
			texts = append(texts, v)
		}
	}
	project := dim(SanitizePlain(ShortenCwd(s.Cwd, s.Home)))
	if s.Branch != "" {
		project += " " + t.fg("accent", "") + " " + t.fg("accent", SanitizePlain(s.Branch))
	}
	if s.Version != "" {
		project += sep + dim("v") + muted(SanitizePlain(strings.TrimPrefix(s.Version, "v")))
	}
	if s.Git.Dirty {
		project += sep + t.fg("success", "+"+strconv.Itoa(s.Git.Added)) + " " + t.fg("error", "-"+strconv.Itoa(s.Git.Removed))
	}
	project = Truncate(project, width)
	if len(texts) > 0 {
		project = joinLR(width, project, dim(strings.Join(texts, "  ")), 2)
	}
	contextText := "?"
	if s.ContextTokens != nil {
		contextText = FormatTokens(*s.ContextTokens)
	}
	capacity := "?"
	if s.ContextWindow > 0 {
		capacity = FormatTokens(s.ContextWindow)
	}
	color := "muted"
	if s.ContextPercent > 90 {
		color = "error"
	} else if s.ContextPercent > 70 {
		color = t.warning()
	}
	window := t.fg(color, contextText) + "/" + dim(capacity)
	input, output, hit, cost := "", "", "", ""
	cache := s.Totals.CacheRead + s.Totals.CacheWrite
	if s.Totals.Input > 0 || cache > 0 {
		input = t.fg("accent", "↑") + muted(FormatTokens(s.Totals.Input))
		if cache > 0 {
			input += "/" + dim(FormatTokens(cache))
		}
	}
	if s.Totals.Output > 0 {
		output = t.fg("accent", "↓") + muted(FormatTokens(s.Totals.Output))
	}
	if s.Totals.CacheRead > 0 && s.HasHit {
		hit = t.fg("accent", "CH") + muted(toFixed(s.LatestHit, 1)+"%")
	}
	if s.Totals.Cost > 0 {
		cost = t.fg("accent", "$") + muted(toFixed(s.Totals.Cost, 3))
	}
	variants := []string{segments(sep, compact(input, output, hit), cost, window), segments(sep, compact(input, output), cost, window), window}
	lines := []string{Truncate(project, width), Truncate(fitSessionLine(width, variants, speed, models, sep), width)}
	var guards []string
	for _, k := range ThirdRowStatusKeys {
		if v := Sanitize(s.Statuses[k]); v != "" {
			guards = append(guards, v)
		}
	}
	if len(guards) > 0 {
		lines = append(lines, Truncate(dim("["+strings.Join(guards, "][")+"]"), width))
	}
	return lines
}
