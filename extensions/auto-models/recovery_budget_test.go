package automodels

import (
	"errors"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestRecoveryBudgetSurvivesIntermediateMessages(t *testing.T) {
	for _, reason := range []string{"toolUse", "stop", "aborted", "length", "error"} {
		t.Run(reason, func(t *testing.T) {
			x, source := modelSwitchFixture(t, 99, 99)
			if err := x.switchOnLimit(source, source.ModelProvider()); err != nil {
				t.Fatal(err)
			}
			target := source.slot
			if _, err := x.messageEnd(sdk.Context{}, map[string]any{"message": map[string]any{"role": "assistant", "stopReason": reason}}); err != nil {
				t.Fatal(err)
			}
			if !x.retryUsed {
				t.Fatalf("%s replenished the current task's allowance", reason)
			}
			if err := x.switchOnLimit(source, source.ModelProvider()); err != nil {
				t.Fatal(err)
			}
			if source.slot != target {
				t.Fatal("repeated recovery switched models again")
			}
			if _, err := x.taskInput(map[string]any{"source": "interactive", "text": "new task"}, func() (bool, error) { return true, nil }); err != nil {
				t.Fatal(err)
			}
			if x.retryUsed || x.retryPending {
				t.Fatal("fresh explicit task did not start a new budget")
			}
		})
	}
}

func TestRecoveryBudgetInputProvenance(t *testing.T) {
	failure := errors.New("host unavailable")
	for _, test := range []struct {
		name, source, behavior, text string
		idle, resets                 bool
		err                          error
	}{
		{name: "interactive", source: "interactive", text: "new task", idle: true, resets: true},
		{name: "rpc", source: "rpc", text: "new task", idle: true, resets: true},
		{name: "synthetic", source: "extension", text: "Continue the interrupted task.", idle: true},
		{name: "unknown", text: "new task", idle: true},
		{name: "steer", source: "interactive", behavior: "steer", text: "correction", idle: true},
		{name: "queued", source: "rpc", behavior: "followUp", text: "next", idle: true},
		{name: "busy", source: "interactive", text: "new task"},
		{name: "empty", source: "interactive", text: "  ", idle: true},
		{name: "failed-state", source: "interactive", text: "new task", idle: true, err: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			x := &extension{retryUsed: true, retryPending: true}
			_, err := x.taskInput(map[string]any{"source": test.source, "text": test.text, "streamingBehavior": test.behavior}, func() (bool, error) { return test.idle, test.err })
			if !errors.Is(err, test.err) {
				t.Fatalf("lost host failure: %v", err)
			}
			if x.retryUsed == test.resets || x.retryPending == test.resets {
				t.Fatalf("used=%v pending=%v, resets=%v", x.retryUsed, x.retryPending, test.resets)
			}
		})
	}
}
