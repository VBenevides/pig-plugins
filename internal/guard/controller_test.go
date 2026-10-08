package guard

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/lancet"
)

func TestControllerUsesPersistedModeForUncertainty(t *testing.T) {
	file := filepath.Join(t.TempDir(), SettingsFileName)
	if err := SaveSettings(file, Change{Mode: new(Strict), Lancet: new(true)}); err != nil {
		t.Fatal(err)
	}
	scorer := &fakeScorer{result: verdict(lancet.Review, 0.8921, "uncertainty-band")}
	gate := NewGate(LoadSettings(file), scorer)
	controller := &Controller{Gate: gate, Settings: file}
	announced := 0
	d := &dialog{answer: true}
	call := Call{Tool: "bash", Input: bash("echo review"), HasUI: true, Confirm: d.confirm}
	judge := func() Decision { return controller.Check(context.Background(), call, func() { announced++ }) }
	if got := judge(); !got.Block || !strings.Contains(got.Reason, "strict mode") || len(d.asked) != 0 {
		t.Fatalf("strict = %+v, dialogs=%q", got, d.asked)
	}
	// A different session/handler saves the same explicit mode. The controller
	// which actually handles tool_call must not retain its strict startup snapshot.
	if err := SaveSettings(file, Change{Mode: new(Interactive)}); err != nil {
		t.Fatal(err)
	}
	if got := judge(); got.Block || len(d.asked) != 1 {
		t.Fatalf("persisted interactive = %+v, dialogs=%q", got, d.asked)
	}
	if gate.Mode() != Interactive || !gate.LancetOn() || announced != 1 || controller.Chip() != "smart-approve-lancet interactive - lancet on" {
		t.Fatalf("runtime disagrees with settings: chip=%q, announced=%d", controller.Chip(), announced)
	}
	d.answer = false
	if got := judge(); !got.Block || !strings.Contains(got.Reason, "user denied") || len(d.asked) != 2 || announced != 1 {
		t.Fatalf("interactive denial = %+v, dialogs=%q, announced=%d", got, d.asked, announced)
	}
	call.HasUI = false
	if got := judge(); !got.Block || !strings.Contains(got.Reason, "no UI") || len(d.asked) != 2 {
		t.Fatalf("headless = %+v, dialogs=%q", got, d.asked)
	}
	if err := SaveSettings(file, Change{Mode: new(Strict)}); err != nil {
		t.Fatal(err)
	}
	call.HasUI = true
	if got := judge(); !got.Block || !strings.Contains(got.Reason, "strict mode") || len(d.asked) != 2 || announced != 2 {
		t.Fatalf("persisted strict = %+v, dialogs=%q, announced=%d", got, d.asked, announced)
	}
}

func TestControllerSettingsFailureNeverDisablesScoring(t *testing.T) {
	file := filepath.Join(t.TempDir(), SettingsFileName)
	if err := SaveSettings(file, Change{Mode: new(Interactive), Lancet: new(true)}); err != nil {
		t.Fatal(err)
	}
	scorer := &fakeScorer{err: errors.New("runtime unavailable")}
	gate := NewGate(LoadSettings(file), scorer)
	controller := &Controller{Gate: gate, Settings: file}
	if err := os.WriteFile(file, []byte(`{"mode":"bogus"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &dialog{answer: true}
	call := Call{Tool: "bash", Input: bash("echo safe"), HasUI: true, Confirm: d.confirm}
	got := controller.Check(context.Background(), call, nil)
	if !got.Block || !strings.Contains(got.Reason, "cannot load current settings") || len(d.asked) != 0 || !gate.LancetOn() {
		t.Fatalf("invalid settings = %+v, lancet=%v, dialogs=%q", got, gate.LancetOn(), d.asked)
	}
	if err := SaveSettings(file, Change{Mode: new(Interactive), Lancet: new(true)}); err != nil {
		t.Fatal(err)
	}
	got = controller.Check(context.Background(), call, nil)
	if !got.Block || !strings.Contains(got.Reason, "runtime unavailable") || len(d.asked) != 0 {
		t.Fatalf("unavailable scorer after repair = %+v, dialogs=%q", got, d.asked)
	}
}

func TestGateKeepsClassifierBandsAtRoundedScoreBoundaries(t *testing.T) {
	// The pinned classifier cuts RAW logits at these inclusive boundaries. Its
	// calibrated score is display-only: four-decimal rounding loses the boundary.
	const review = 0.2956467684116474
	const risky = 0.991200665739696
	for _, tc := range []struct {
		name  string
		class lancet.Classification
		score float64
		asks  int
		block bool
	}{
		{"below review", lancet.NotFlagged, math.Nextafter(review, 0), 0, false},
		{"at review", lancet.Review, review, 1, false},
		{"below risky", lancet.Review, math.Nextafter(risky, 0), 1, false},
		{"at risky", lancet.Risky, risky, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &dialog{answer: true}
			scorer := &fakeScorer{result: verdict(tc.class, tc.score, "")}
			got := check(newGate(Interactive, true, scorer), d, "bash", bash("echo harmless"), true)
			if got.Block != tc.block || len(d.asked) != tc.asks {
				t.Fatalf("verdict=%s, displayed score=%s: %+v, dialogs=%q", tc.class, ScoreText(&tc.score), got, d.asked)
			}
		})
	}
}
