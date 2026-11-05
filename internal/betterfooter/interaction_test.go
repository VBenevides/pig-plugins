package betterfooter

import (
	"strings"
	"testing"
	"time"
)

func TestFooterInteractionBeforeMode(t *testing.T) {
	state := RenderState{Speed: 17, Statuses: map[string]string{"interaction-page": "2/5"}}
	rows := RenderFooter(state, 240, Theme{}, time.Now())
	if !strings.Contains(rows[0], "Interaction: 2/5 - Mode: ACT") {
		t.Fatalf("missing page before mode: %q", rows[0])
	}
	if strings.Contains(rows[1], "Interaction") {
		t.Fatalf("page left in speed row: %q", rows[1])
	}
	delete(state.Statuses, "interaction-page")
	if row := RenderFooter(state, 240, Theme{}, time.Now())[0]; strings.Contains(row, "Interaction") {
		t.Fatalf("stale interaction: %q", row)
	}
}
