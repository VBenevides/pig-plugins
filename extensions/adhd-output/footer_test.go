package adhdoutput

import (
	"strings"
	"testing"
	"time"

	mode "github.com/VBenevides/pig-plugins/internal/adhdoutput"
	"github.com/VBenevides/pig-plugins/internal/betterfooter"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
)

type statusRecorder map[string]string

func (r statusRecorder) SetStatus(key, text string) {
	if text == "" {
		delete(r, key)
	} else {
		r[key] = text
	}
}

func TestSharedNormalFooterAndNarrowWidths(t *testing.T) {
	r := statusRecorder{}
	footerstatus.Set(r, "adhd-test-other", "OTHER STATUS")
	footerstatus.Set(r, mode.StatusKey, mode.Badge)
	defer footerstatus.Set(r, "adhd-test-other", "")
	defer footerstatus.Set(r, mode.StatusKey, "")
	tokens := 2000
	state := betterfooter.RenderState{Cwd: "/project", Provider: "mock", Model: "mock-model", ContextTokens: &tokens, ContextWindow: 100000, Speed: 42, Statuses: footerstatus.Snapshot()}
	lines := betterfooter.RenderFooter(state, 180, betterfooter.Theme{}, time.Unix(1000, 0))
	for _, text := range []string{mode.Badge, "OTHER STATUS", "mock/mock-model", "42t/s", "2.0k/100k"} {
		if !strings.Contains(strings.Join(lines, "\n"), text) {
			t.Fatalf("normal fused footer lost %q: %v", text, lines)
		}
	}
	for _, width := range []int{0, 1, 4, 8, 12, 20, 40} {
		for _, line := range betterfooter.RenderFooter(state, width, betterfooter.Theme{}, time.Unix(1000, 0)) {
			if betterfooter.VisibleWidth(line) > width {
				t.Fatalf("narrow footer overflow at %d: %q", width, line)
			}
		}
	}
	footerstatus.Set(r, mode.StatusKey, "")
	if _, ok := footerstatus.Snapshot()[mode.StatusKey]; ok {
		t.Fatal("ADHD badge did not clear")
	}
	if r["adhd-test-other"] != "OTHER STATUS" {
		t.Fatal("disable removed another contribution")
	}
}
