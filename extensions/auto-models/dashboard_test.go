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

func TestDashboardShowsAccountsInTwoColumnsWhenWide(t *testing.T) {
	lines := []string{"Active model: x", "", "── Account (a) one ──", "  A1", "  A2", "  A3", "", "── Account (b) two ──", "  B1", "", "── Account (c) three ──", "  🔑 C1"}
	d := &dashboard{title: "Usage", lines: lines}
	wide := strings.Join(d.Render(140), "\n")
	if !strings.Contains(wide, "Active model: x") || !strings.Contains(wide, "── Account (b) two ──") {
		t.Fatalf("content lost:\n%s", wide)
	}
	for _, row := range strings.Split(wide, "\n") {
		if strings.Contains(row, "Account (a) one") && !strings.Contains(row, "Account (b) two") {
			t.Fatalf("first two accounts must share a row:\n%s", wide)
		}
		if strings.Contains(row, columnGap) && cellWidth(row) > 140 {
			t.Fatalf("row too wide (%d): %q", cellWidth(row), row)
		}
	}
	narrow := d.Render(80)
	for _, row := range narrow {
		if strings.Contains(row, columnGap) {
			t.Fatalf("narrow view must stay one column: %q", row)
		}
	}
	if got := strings.Join(narrow, "\n"); !strings.Contains(got, "A3") || !strings.Contains(got, "C1") {
		t.Fatalf("narrow view lost lines:\n%s", got)
	}
	if _, err := d.HandleInput("G"); err != nil {
		t.Fatal(err)
	}
}
