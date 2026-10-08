package betterfooter

import "time"

// minSpeedWindow is the shortest streaming span a rate is computed over.
const minSpeedWindow = 50 * time.Millisecond

// liveRenderGap is the least time between two live estimate refreshes: at most ten a second.
const liveRenderGap = 100 * time.Millisecond

// StreamUsage is the part of an assistant message's usage the speed needs.
type StreamUsage struct {
	Output    int
	Reasoning int
}

type estimate struct {
	tokens                             float64
	startedAt, renderedAt, lastDeltaAt time.Duration
}

// SpeedTracker measures output tokens per second while an assistant reply streams. Timing starts at the first
// delta, not at message_start: the gap before it is queueing and prompt processing, and for providers that hide
// reasoning, the reasoning itself. Times are offsets from any fixed monotonic origin. It is not safe for
// concurrent use.
type SpeedTracker struct {
	// Live enables the character-based estimate while streaming; the footer only shows it when mounted.
	Live bool
	// Speed is the last shown rate in tokens per second, 0 when there is none.
	Speed float64
	// Estimated marks a rate that comes from the character heuristic rather than from reported usage.
	Estimated bool

	est              *estimate
	firstDelta       *time.Duration
	firstAnswerDelta *time.Duration
	lastModelUpdate  *time.Duration
	hasToolCall      bool
}

// MessageStart resets the per-reply timing; the shown rate stays until the next one is known.
func (t *SpeedTracker) MessageStart() {
	t.est, t.firstDelta, t.firstAnswerDelta, t.lastModelUpdate, t.hasToolCall = nil, nil, nil, nil, false
}

func estimateRate(e *estimate, end time.Duration) (float64, bool) {
	sec := (end - e.startedAt).Seconds()
	if e.tokens > 0 && end-e.startedAt > minSpeedWindow {
		return e.tokens / sec, true
	}
	return 0, false
}

// Delta records one streamed update of the assistant message. kind is the assistantMessageEvent type
// ("text_delta", "thinking_delta", "toolcall_start", "toolcall_delta", "toolcall_end", ...). It returns true
// when the shown rate changed and the footer should render again.
func (t *SpeedTracker) Delta(kind, delta string, now time.Duration) bool {
	changed := false
	if kind == "text_delta" || kind == "thinking_delta" || kind == "toolcall_delta" {
		changed = t.live(delta, now)
	}
	// Even a tool-call start without argument deltas disqualifies this reply (an aborted call, or a provider that
	// delivers complete arguments).
	if kind == "toolcall_start" || kind == "toolcall_delta" || kind == "toolcall_end" {
		t.hasToolCall = true
		return changed
	}
	// Only text and thinking deltas belong to the reply speed. Block-end, done and abort events can land well
	// after the last token: an aborted reply would otherwise count its idle stall as generation time.
	if kind != "text_delta" && kind != "thinking_delta" {
		return changed
	}
	if t.firstDelta == nil {
		t.firstDelta = new(now)
	}
	if kind != "thinking_delta" && t.firstAnswerDelta == nil {
		t.firstAnswerDelta = new(now)
	}
	if t.lastModelUpdate == nil {
		t.lastModelUpdate = new(now)
	} else {
		*t.lastModelUpdate = now
	}
	return changed
}

func (t *SpeedTracker) live(delta string, now time.Duration) bool {
	if delta == "" || !t.Live {
		return false
	}
	if t.est == nil {
		// The first chunk only starts the clock: its tokens were generated before timing began, so counting
		// them would overstate early samples.
		t.est = &estimate{startedAt: now, renderedAt: now, lastDeltaAt: now}
		return false
	}
	// A language-aware heuristic, not a tokenizer: roughly four ASCII characters per token, one for non-ASCII.
	for _, r := range delta {
		if r < 128 {
			t.est.tokens += 0.25
		} else {
			t.est.tokens++
		}
	}
	t.est.lastDeltaAt = now
	if now-t.est.renderedAt < liveRenderGap {
		return false
	}
	rate, ok := estimateRate(t.est, now)
	if !ok {
		return false
	}
	t.Speed, t.Estimated = rate, true
	t.est.renderedAt = now
	return true
}

// measure is the speed over the streamed part of a reply: the reported output tokens, minus reasoning tokens
// (those may be generated before or between the streamed deltas, and a reasoning summary is far shorter than the
// reasoning it summarizes), over the time from the first delta that carries them to the last model update.
func (t *SpeedTracker) measure(usage StreamUsage) (float64, bool) {
	tokens := usage.Output - usage.Reasoning
	start := t.firstDelta
	if usage.Reasoning > 0 {
		start = t.firstAnswerDelta
	}
	if start == nil || t.lastModelUpdate == nil || tokens <= 0 {
		return 0, false
	}
	// End at the last model delta, not at a delayed message_end callback.
	span := *t.lastModelUpdate - *start
	if span <= minSpeedWindow {
		return 0, false
	}
	return float64(tokens) / span.Seconds(), true
}

// MessageEnd finalizes the rate of an assistant reply and resets the timing. Providers report whole-message
// output usage, not separate counts for text and tool arguments, so only replies without tool calls can use the
// reported usage; a mixed reply keeps its marked estimate, flushed through its last chunk.
func (t *SpeedTracker) MessageEnd(usage StreamUsage, hasToolBlock bool) {
	if !t.hasToolCall && !hasToolBlock {
		if rate, ok := t.measure(usage); ok {
			t.Speed, t.Estimated = rate, false
			t.MessageStart()
			return
		}
	}
	if t.est != nil {
		if rate, ok := estimateRate(t.est, t.est.lastDeltaAt); ok {
			t.Speed, t.Estimated = rate, true
		}
	}
	t.MessageStart()
}
