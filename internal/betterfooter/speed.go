package betterfooter

import "time"

const liveRenderGap = 100 * time.Millisecond

// Output includes reasoning tokens where the provider reports them as output.
// Reasoning is retained as usage metadata, not added again to Output.
type StreamUsage struct {
	Output    int
	Reasoning int
}

// SpeedTracker measures cumulative model output over an entire agent run.
// Elapsed wall time includes prompt processing, tools, and pauses between replies.
// Times are offsets from a fixed monotonic origin. It is not safe for concurrent use.
type SpeedTracker struct {
	Live      bool
	Speed     float64
	Estimated bool

	active        bool
	messageActive bool
	startedAt     time.Duration
	renderedAt    time.Duration
	cumTokens     float64
	cumEstimated  bool
	pendingTokens float64
}

// AgentStart resets the displayed rate and cumulative sample for a new interaction.
func (t *SpeedTracker) AgentStart(now time.Duration) {
	t.active, t.messageActive = true, false
	t.startedAt, t.renderedAt = now, now
	t.cumTokens, t.pendingTokens = 0, 0
	t.Speed, t.Estimated, t.cumEstimated = 0, false, false
}

// MessageStart resets only the provisional tokens for the next assistant reply.
func (t *SpeedTracker) MessageStart() {
	if !t.active {
		return
	}
	t.messageActive = true
	t.pendingTokens = 0
}

// Delta estimates model-generated text, thinking, and tool arguments. Tool
// results, block endings, and completion events do not add output tokens.
func (t *SpeedTracker) Delta(kind, delta string, now time.Duration) bool {
	if !t.active || !t.messageActive || !t.Live || delta == "" {
		return false
	}
	if kind != "text_delta" && kind != "thinking_delta" && kind != "toolcall_delta" {
		return false
	}
	// A heuristic rather than a tokenizer: four ASCII characters per token,
	// one token per non-ASCII character. Reported usage replaces it at message_end.
	for _, r := range delta {
		if r < 128 {
			t.pendingTokens += 0.25
		} else {
			t.pendingTokens++
		}
	}
	if now-t.renderedAt < liveRenderGap {
		return false
	}
	t.renderedAt = now
	return t.Refresh(now)
}

// MessageEnd replaces this reply's estimate with reported output, never adding
// both. If usage is unavailable, preserve the estimate and its marker.
func (t *SpeedTracker) MessageEnd(usage StreamUsage, now time.Duration) {
	if !t.active || !t.messageActive {
		return
	}
	if usage.Output > 0 {
		t.cumTokens += float64(usage.Output)
	} else {
		t.cumTokens += t.pendingTokens
		t.cumEstimated = t.cumEstimated || t.pendingTokens > 0
	}
	t.pendingTokens = 0
	t.messageActive = false
	t.Refresh(now)
}

// Refresh updates elapsed time even while a tool runs. After AgentEnd, it
// leaves the final rate unchanged until the next AgentStart.
func (t *SpeedTracker) Refresh(now time.Duration) bool {
	if !t.active || now <= t.startedAt {
		return false
	}
	rate := (t.cumTokens + t.pendingTokens) / (now - t.startedAt).Seconds()
	estimated := t.cumEstimated || t.pendingTokens > 0
	changed := rate != t.Speed || estimated != t.Estimated
	t.Speed, t.Estimated = rate, estimated
	return changed
}

// AgentEnd includes any interrupted reply's provisional tokens, then freezes.
func (t *SpeedTracker) AgentEnd(now time.Duration) {
	if !t.active {
		return
	}
	if t.messageActive {
		t.MessageEnd(StreamUsage{}, now)
	}
	t.Refresh(now)
	t.active = false
}
