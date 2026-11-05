package betterfooter

import (
	"strings"
	"testing"
	"time"
)

func TestFooterInteractionAfterSpeed(t *testing.T) {
	state := RenderState{Speed: 17, Statuses: map[string]string{"interaction-page": "2/5"}}
	rows := RenderFooter(state, 240, Theme{}, time.Now())
	if !strings.Contains(rows[1], "17t/s - Interaction 2/5") {
		t.Fatalf("missing page after speed: %q", rows[1])
	}
	if strings.Contains(rows[0], "2/5") {
		t.Fatalf("duplicated page status: %q", rows)
	}
	state.Speed = 0
	if row := RenderFooter(state, 240, Theme{}, time.Now())[1]; !strings.Contains(row, "Interaction 2/5") {
		t.Fatalf("page absent before speed available: %q", row)
	}
	delete(state.Statuses, "interaction-page")
	if row := RenderFooter(state, 240, Theme{}, time.Now())[1]; strings.Contains(row, "Interaction") {
		t.Fatalf("stale interaction: %q", row)
	}
}
