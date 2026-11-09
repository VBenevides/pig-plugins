package cursor

import (
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

func TestPiUsageExcludesCachedTokensFromInput(t *testing.T) {
	got := piUsage(cursorbridge.TokenUsage{InputTokens: 23827, OutputTokens: 165, CacheReadTokens: 18048, ReasoningTokens: 67})
	if got["input"] != int64(5779) || got["cacheRead"] != int64(18048) || got["output"] != int64(165) || got["cacheWrite"] != int64(0) {
		t.Fatalf("%v", got)
	}
	// PiG adds the four fields, so the total equals Cursor's input plus output.
	if got["totalTokens"] != int64(23827+165) {
		t.Fatalf("total %v", got["totalTokens"])
	}
}

func TestPiUsageNeverGoesNegative(t *testing.T) {
	got := piUsage(cursorbridge.TokenUsage{InputTokens: 5, OutputTokens: 1, CacheReadTokens: 9})
	if got["input"] != int64(0) {
		t.Fatalf("%v", got)
	}
}

func TestTurnMessageCarriesUsageWithUncachedInput(t *testing.T) {
	r := &fakeRunner{catalog: grokCatalog, events: []cursorbridge.Event{{Text: "hi"}}, outcome: cursorbridge.Outcome{
		Text: "hi", Usage: &cursorbridge.TokenUsage{InputTokens: 1000, OutputTokens: 10, CacheReadTokens: 600}}}
	s, _ := newService(map[string]*fakeRunner{"k": r})
	final := runFollowUpOn(t, s, "grok-4.6-fast", request("go"), "low")
	usage, _ := final["usage"].(map[string]any)
	if usage["input"] != float64(400) || usage["cacheRead"] != float64(600) || usage["output"] != float64(10) {
		t.Fatalf("usage %v", final["usage"])
	}
}

func TestUsageReportShowsCachedUncachedAndOutputPerModel(t *testing.T) {
	var tally usageTally
	if got := tally.report(); !strings.Contains(got, "No Cursor usage") {
		t.Fatalf("empty: %q", got)
	}
	tally.add("grok-4.6-fast (low)", cursorbridge.Outcome{
		Usage:      &cursorbridge.TokenUsage{InputTokens: 1000, OutputTokens: 40, CacheReadTokens: 600, ReasoningTokens: 25},
		ModelCalls: []cursorbridge.TokenUsage{{}, {}},
	})
	tally.add("grok-4.6-fast (low)", cursorbridge.Outcome{Usage: &cursorbridge.TokenUsage{InputTokens: 500, OutputTokens: 10, CacheReadTokens: 100}})
	tally.add("grok-4.6-fast (low)", cursorbridge.Outcome{}) // no usage reported
	got := tally.report()
	for _, want := range []string{"grok-4.6-fast (low)", "input 1500 (cached 700 = 47%, uncached 800)", "output 50 (reasoning 25)", "3 turns, 2 model calls"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	tally.add("other", cursorbridge.Outcome{Usage: &cursorbridge.TokenUsage{InputTokens: 10, OutputTokens: 1}})
	if !strings.Contains(tally.report(), "All Cursor models") {
		t.Fatal("no total for several models")
	}
	tally.reset()
	if got := tally.report(); !strings.Contains(got, "No Cursor usage") {
		t.Fatalf("reset: %q", got)
	}
}

func TestServiceTalliesEachTurn(t *testing.T) {
	r := &fakeRunner{catalog: grokCatalog, events: []cursorbridge.Event{{Text: "hi"}}, outcome: cursorbridge.Outcome{
		Text: "hi", Usage: &cursorbridge.TokenUsage{InputTokens: 100, OutputTokens: 5, CacheReadTokens: 40}}}
	s, _ := newService(map[string]*fakeRunner{"k": r})
	runFollowUpOn(t, s, "grok-4.6-fast", request("go"), "low")
	runFollowUpOn(t, s, "grok-4.6-fast", request("again"), "low")
	if got := s.usage.report(); !strings.Contains(got, "input 200 (cached 80 = 40%, uncached 120)") || !strings.Contains(got, "2 turns") {
		t.Fatalf("%s", got)
	}
}
