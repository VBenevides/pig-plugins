package guard

import (
	"context"
	"os"
	"testing"
)

func TestGuardModePersistsAndRestoresPolicy(t *testing.T) {
	h := newHarness(t)
	h.lancet.model = ModelInfo{Verified: true}
	h.run("lancet on")
	h.lancet.calls = nil
	h.run("mode off")
	restarted := &Controller{Gate: NewGate(Settings{}, h.lancet), Settings: h.file, Modes: h.ctl.Modes}
	for _, call := range []Call{
		{Tool: "bash", Input: bash("rm -rf /")},
		{Tool: "bash", Input: bash("echo model check")},
		{Tool: "write", Input: map[string]any{"path": ".env"}},
		{Tool: "read", Input: map[string]any{"path": ".env"}},
	} {
		if got := restarted.Check(context.Background(), call, nil); got.Block {
			t.Fatal(got)
		}
	}
	if len(h.lancet.calls) != 0 {
		t.Fatal(h.lancet.calls)
	}
	h.run("mode strict")
	if !LoadSettings(h.file).Lancet {
		t.Fatal("mode change disabled profile scoring")
	}
	if got := restarted.Check(context.Background(), Call{Tool: "bash", Input: bash("rm -rf /")}, nil); !got.Block {
		t.Fatal("hard block not restored")
	}
}

func TestGuardModeSaveFailureKeepsPolicy(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.file, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	h.ctl.Modes.Path = h.file + "/impossible"
	h.run("mode off")
	if h.gate.access != ModeLax || h.announce != 0 || h.levels[0] != "error" {
		t.Fatal(h.notices)
	}
}
