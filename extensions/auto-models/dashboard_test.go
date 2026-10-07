package automodels

import (
	"strings"
	"testing"
)

func TestDashboardScrollsAndClosesWithQ(t *testing.T) {
	lines := make([]string, dashboardRows+10)
	for i := range lines {
		lines[i] = "line"
	}
	lines[len(lines)-1] = "last"
	d := &dashboard{title: "Usage", lines: lines}
	for _, key := range []string{"G", "j"} { // j at the end must clamp
		if r, err := d.HandleInput(key); err != nil || r.Done {
			t.Fatalf("%q: %+v %v", key, r, err)
		}
	}
	if out := strings.Join(d.Render(40), "\n"); !strings.Contains(out, "last") {
		t.Fatalf("end of content not visible:\n%s", out)
	}
	if _, err := d.HandleInput("g"); err != nil || d.offset != 0 {
		t.Fatal("g must return to top")
	}
	for _, key := range []string{"q", "Q", "\x1b"} {
		if r, err := d.HandleInput(key); err != nil || !r.Done {
			t.Fatalf("%q must close: %+v %v", key, r, err)
		}
	}
}
