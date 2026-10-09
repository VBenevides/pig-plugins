package guard

import (
	"context"
	"os"
	"testing"
)

func TestGuardSwitchPersistsAndRestoresPolicy(t *testing.T) {
	h := newHarness(t)
	h.run("auto")
	h.lancet.model = ModelInfo{Verified: true}
	h.run("lancet on")
	h.lancet.calls = nil
	h.run("off")
	if h.ctl.Chip() != "smart-approve-lancet off" || !LoadSettings(h.file).Disabled {
		t.Fatalf("off state: chip=%q settings=%+v", h.ctl.Chip(), LoadSettings(h.file))
	}
	restarted := &Controller{Gate: NewGate(LoadSettings(h.file), h.lancet), Settings: h.file}
	for _, call := range []Call{
		{Tool: "bash", Input: bash("rm -rf /")},
		{Tool: "bash", Input: bash("echo model check")},
		{Tool: "write", Input: map[string]any{"path": ".env"}},
		{Tool: "edit", Input: map[string]any{"path": ".env"}},
	} {
		if got := restarted.Check(context.Background(), call, nil); got.Block {
			t.Fatalf("off blocked %s: %+v", call.Tool, got)
		}
	}
	if len(h.lancet.calls) != 0 {
		t.Fatalf("off scored: %v", h.lancet.calls)
	}
	h.run("on")
	if h.ctl.Chip() != "smart-approve-lancet on - auto - lancet on" || LoadSettings(h.file).Disabled {
		t.Fatalf("on did not preserve mode/scoring: %q", h.ctl.Chip())
	}
	if got := restarted.Check(context.Background(), Call{Tool: "bash", Input: bash("rm -rf /")}, nil); !got.Block {
		t.Fatal("persisted on did not restore hard blocks")
	}
}

func TestGuardSwitchSaveFailureKeepsPolicyEnabled(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.file, []byte("[1]"), 0600); err != nil {
		t.Fatal(err)
	}
	h.run("off")
	if !h.gate.Enabled() || h.announce != 0 || h.levels[0] != "error" {
		t.Fatalf("failed save changed guard: enabled=%v announcements=%d notices=%v", h.gate.Enabled(), h.announce, h.notices)
	}
}
