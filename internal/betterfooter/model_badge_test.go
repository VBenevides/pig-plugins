package betterfooter

import (
	"strings"
	"testing"
	"time"
)

func TestModelBadgePrecedesProviderWithoutDuplicateQuota(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, badge := range []string{"🧠 primary", "⚡ fallback"} {
		for _, width := range []int{60, 120, 240} {
			state := RenderState{
				Cwd: "/project", Provider: "openai-codex", Model: "gpt-6.1-sol",
				Statuses: map[string]string{"auto-model": badge, "auto-model-usage": "Checking quota…", "auto-model-quota": "5h 53%", "adhd": "● ADHD ON"},
				Quota:    ProviderQuota{Windows: []RateWindow{{Percent: 53, HasReset: true, ResetSec: 18000, CapturedAt: now}}},
			}
			lines := RenderFooter(state, width, Theme{}, now)
			if strings.Contains(lines[0], badge) || strings.Contains(lines[0], "53%") || !strings.Contains(lines[0], "ADHD ON") {
				t.Fatalf("unexpected top row: %q", lines[0])
			}
			if !strings.Contains(lines[1], badge+" openai-codex/gpt-6.1-sol") {
				t.Fatalf("badge must immediately precede provider/model: %q", lines[1])
			}
			if strings.Count(strings.Join(lines, "\n"), "53%") != 1 {
				t.Fatalf("quota must appear exactly once: %q", lines)
			}
		}
	}
}
