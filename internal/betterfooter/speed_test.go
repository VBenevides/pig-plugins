package betterfooter

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestInteractionSpeedAccumulatesRepliesAndToolTime(t *testing.T) {
	tracker := SpeedTracker{Live: true}
	tracker.AgentStart(10 * time.Second)
	tracker.MessageStart()
	tracker.Delta("thinking_delta", strings.Repeat("abcd", 40), 12*time.Second)
	tracker.Delta("toolcall_delta", strings.Repeat("abcd", 20), 13*time.Second)
	if tracker.Speed != 20 || !tracker.Estimated {
		t.Fatalf("live speed = %v estimated=%v", tracker.Speed, tracker.Estimated)
	}
	// Reported output replaces the 60-token estimate, including reasoning once.
	tracker.MessageEnd(StreamUsage{Output: 100, Reasoning: 40}, 14*time.Second)
	if tracker.Speed != 25 || tracker.Estimated {
		t.Fatalf("first reply speed = %v estimated=%v", tracker.Speed, tracker.Estimated)
	}
	tracker.Delta("tool_result", strings.Repeat("abcd", 10000), 15*time.Second)
	tracker.Refresh(20 * time.Second)
	if tracker.Speed != 10 {
		t.Fatalf("tool execution must add time, not tokens: %v", tracker.Speed)
	}
	tracker.MessageStart()
	tracker.Delta("text_delta", strings.Repeat("abcd", 40), 22*time.Second)
	if math.Abs(tracker.Speed-140.0/12) > 1e-9 || !tracker.Estimated {
		t.Fatalf("cumulative live speed = %v estimated=%v", tracker.Speed, tracker.Estimated)
	}
	tracker.MessageEnd(StreamUsage{Output: 200}, 25*time.Second)
	if tracker.Speed != 20 || tracker.Estimated {
		t.Fatalf("combined speed = %v estimated=%v", tracker.Speed, tracker.Estimated)
	}
	tracker.AgentEnd(30 * time.Second)
	if tracker.Speed != 15 {
		t.Fatalf("final interaction speed = %v", tracker.Speed)
	}
	tracker.Refresh(100 * time.Second)
	tracker.AgentEnd(101 * time.Second)
	tracker.MessageStart()
	tracker.Delta("text_delta", strings.Repeat("x", 1000), 102*time.Second)
	tracker.MessageEnd(StreamUsage{Output: 1000}, 103*time.Second)
	if tracker.Speed != 15 {
		t.Fatalf("completed interaction did not hold its rate: %v", tracker.Speed)
	}
	tracker.AgentStart(110 * time.Second)
	if tracker.Speed != 0 || tracker.Estimated {
		t.Fatal("new interaction did not reset display")
	}
	tracker.MessageStart()
	tracker.MessageEnd(StreamUsage{Output: 50}, 112*time.Second)
	tracker.AgentEnd(115 * time.Second)
	if tracker.Speed != 10 {
		t.Fatalf("new interaction included previous tokens or idle time: %v", tracker.Speed)
	}
}

func TestInteractionSpeedSmallSamplesAndDuplicateEnd(t *testing.T) {
	tracker := SpeedTracker{}
	tracker.AgentStart(0)
	tracker.MessageStart()
	tracker.MessageEnd(StreamUsage{Output: 1}, 100*time.Millisecond)
	tracker.MessageEnd(StreamUsage{Output: 1000}, 200*time.Millisecond)
	tracker.AgentEnd(time.Second)
	if tracker.Speed != 1 || tracker.Estimated {
		t.Fatalf("small reported sample or duplicate end: %v", tracker.Speed)
	}
}

func TestInteractionSpeedMissingUsageAndInterruptedReply(t *testing.T) {
	for _, finishReply := range []bool{true, false} {
		tracker := SpeedTracker{Live: true}
		tracker.AgentStart(0)
		tracker.MessageStart()
		tracker.Delta("toolcall_delta", strings.Repeat("abcd", 20), time.Second)
		if finishReply {
			tracker.MessageEnd(StreamUsage{}, 2*time.Second)
		}
		tracker.AgentEnd(4 * time.Second)
		if tracker.Speed != 5 || !tracker.Estimated {
			t.Fatalf("missing usage/abort rate = %v estimated=%v", tracker.Speed, tracker.Estimated)
		}
	}
}

func TestInteractionSpeedIgnoresNonModelEvents(t *testing.T) {
	tracker := SpeedTracker{Live: true}
	tracker.AgentStart(0)
	tracker.MessageStart()
	for _, kind := range []string{"tool_result", "toolcall_start", "toolcall_end", "done", "abort"} {
		if tracker.Delta(kind, strings.Repeat("abcd", 10000), time.Second) {
			t.Fatalf("non-model event %q changed rate", kind)
		}
	}
	tracker.AgentEnd(2 * time.Second)
	if tracker.Speed != 0 || tracker.Estimated {
		t.Fatal("non-model events counted as output")
	}
}

func TestInteractionSpeedZeroElapsedAndThrottledTokens(t *testing.T) {
	tracker := SpeedTracker{Live: true}
	tracker.AgentStart(time.Second)
	tracker.MessageStart()
	tracker.Delta("text_delta", "abcd", time.Second)
	tracker.Delta("text_delta", "abcd", time.Second+50*time.Millisecond)
	if tracker.Refresh(time.Second) || tracker.Speed != 0 {
		t.Fatal("zero elapsed time must not produce an invalid rate")
	}
	tracker.AgentEnd(2 * time.Second)
	if tracker.Speed != 2 || !tracker.Estimated {
		t.Fatal("throttled chunks lost tokens", tracker.Speed)
	}
}
