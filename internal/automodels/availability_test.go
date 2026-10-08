package automodels

import (
	"math"
	"testing"
)

func TestCodexAvailability(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		usage *CodexUsage
		want  int // -1 unknown, 0 unavailable, 1 available
	}{
		{"missing", nil, -1},
		{"missing limits", &CodexUsage{}, -1},
		{"empty limits", &CodexUsage{RateLimit: &CodexRateLimit{Allowed: true}}, -1},
		{"available", &CodexUsage{RateLimit: &CodexRateLimit{Allowed: true, PrimaryWindow: &CodexWindow{UsedPercent: 30}, SecondaryWindow: &CodexWindow{UsedPercent: 60}}}, 1},
		{"weekly exhausted", &CodexUsage{RateLimit: &CodexRateLimit{Allowed: true, PrimaryWindow: &CodexWindow{UsedPercent: 30}, SecondaryWindow: &CodexWindow{UsedPercent: 100}}}, 0},
		{"short exhausted", &CodexUsage{RateLimit: &CodexRateLimit{Allowed: true, PrimaryWindow: &CodexWindow{UsedPercent: 100}}}, 0},
		{"limit reached", &CodexUsage{RateLimit: &CodexRateLimit{Allowed: true, LimitReached: true}}, 0},
		{"not allowed", &CodexUsage{RateLimit: &CodexRateLimit{PrimaryWindow: &CodexWindow{UsedPercent: 30}}}, 0},
		{"invalid usage", &CodexUsage{RateLimit: &CodexRateLimit{Allowed: true, PrimaryWindow: &CodexWindow{UsedPercent: math.NaN()}}}, -1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			got := CodexAvailable(scenario.usage)
			if scenario.want == -1 {
				if got != nil {
					t.Fatal("unknown quota reported as known")
				}
				return
			}
			if got == nil || *got != (scenario.want == 1) {
				t.Fatalf("availability = %v, want %d", got, scenario.want)
			}
		})
	}
}

func TestClaudeAvailabilityRequiresNumericUsage(t *testing.T) {
	if got := ClaudeAvailable(&ClaudeUsage{Limits: []ClaudeLimit{{Kind: "session"}}}); got != nil {
		t.Fatal("missing usage reported as available")
	}
}
